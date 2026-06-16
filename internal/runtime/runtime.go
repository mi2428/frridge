// Package runtime orchestrates Docker containers, host networking, and FRR
// bootstrap files into a runnable lab.
package runtime

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"frridge/internal/config"
	"frridge/internal/docker"
	"frridge/internal/frr"
	labnetlink "frridge/internal/netlink"
	"frridge/internal/state"
)

const (
	labelManaged  = "io.frridge.managed"
	labelLab      = "io.frridge.lab"
	labelRouter   = "io.frridge.router"
	labelTopology = "io.frridge.topology"
)

// Service is the high-level lab lifecycle used by the CLI.
type Service interface {
	Up(ctx context.Context, topologyPath string, opts UpOptions) error
	Down(ctx context.Context, topologyPath string, opts DownOptions) error
	Console(ctx context.Context, topologyPath string, router string, opts ConsoleOptions) error
	Ping(ctx context.Context, topologyPath string, name string) ([]PingResult, error)
}

// UpOptions controls how strictly Up reconciles existing generated state.
type UpOptions struct {
	Recreate bool
	Reseed   bool
}

// DownOptions controls optional cleanup beyond live runtime objects.
type DownOptions struct {
	Purge bool
}

// ConsoleOptions chooses which process is opened inside a running router.
type ConsoleOptions struct {
	Shell bool
}

// PingResult captures one YAML-defined ping check and the raw command output
// produced when it ran inside the source container.
type PingResult struct {
	Name      string
	Router    string
	Namespace string
	Target    string
	Output    string
	ExitCode  int
}

// Manager implements Service by coordinating Docker, netlink, and generated
// FRR config files.
type Manager struct {
	docker  docker.Client
	network labnetlink.Manager
}

// NewManager builds a runtime manager from explicit Docker and networking
// dependencies.
func NewManager(dockerClient docker.Client, networkManager labnetlink.Manager) *Manager {
	return &Manager{
		docker:  dockerClient,
		network: networkManager,
	}
}

// NewDefaultManager wires the production Docker SDK client and netlink
// implementation together.
func NewDefaultManager() (*Manager, error) {
	dockerClient, err := docker.New()
	if err != nil {
		return nil, err
	}
	return NewManager(dockerClient, labnetlink.New()), nil
}

// Up creates or recreates the lab described by topologyPath.
func (m *Manager) Up(ctx context.Context, topologyPath string, opts UpOptions) (err error) {
	if err := requireHostNetworking(); err != nil {
		return err
	}

	topology, err := config.LoadFile(topologyPath)
	if err != nil {
		return err
	}

	if opts.Recreate {
		if err := m.cleanupRuntime(ctx, topology, nil, false); err != nil {
			return err
		}
	} else {
		exists, err := m.runtimeExists(ctx, topology)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("lab %q already has runtime state; re-run with --recreate", topology.Lab.Name)
		}
	}

	if err := os.MkdirAll(topology.RoutersDir(), 0o755); err != nil {
		return fmt.Errorf("create router state dir: %w", err)
	}

	digest, err := topology.Digest()
	if err != nil {
		return err
	}

	snapshot := &state.LabState{
		Lab:            topology.Lab.Name,
		TopologyDigest: digest,
		Containers:     make(map[string]state.ContainerState, len(topology.Routers)),
	}
	prepared := make(map[string]frr.PrepareResult, len(topology.Routers))
	resolvedRouters := topology.ResolveRouters()

	defer func() {
		if err != nil {
			_ = m.cleanupRuntime(ctx, topology, snapshot, false)
		}
	}()

	for _, routerName := range sortedRouterNames(topology.Routers) {
		router := resolvedRouters[routerName]
		prep, prepErr := frr.PrepareRouterFiles(topology.RouterDir(routerName), router.Hostname, opts.Reseed)
		if prepErr != nil {
			return prepErr
		}
		prepared[routerName] = prep

		mounts := append([]docker.Mount{
			{
				Source:   prep.ConfigDir,
				Target:   "/etc/frr",
				ReadOnly: false,
			},
		}, toDockerMounts(router.Mounts)...)

		containerID, createErr := m.docker.CreateContainer(ctx, docker.ContainerSpec{
			Name:       containerName(topology.Lab.Name, routerName),
			Image:      router.Image,
			Hostname:   router.Hostname,
			Command:    router.Command,
			Env:        router.Env,
			Labels:     routerLabels(topology.Lab.Name, routerName, digest),
			Sysctls:    router.Sysctls,
			Mounts:     mounts,
			Privileged: router.Privileged,
		})
		if createErr != nil {
			return createErr
		}
		if startErr := m.docker.StartContainer(ctx, containerID); startErr != nil {
			return startErr
		}

		inspect, inspectErr := m.docker.InspectContainer(ctx, containerID)
		if inspectErr != nil {
			return inspectErr
		}

		snapshot.Containers[routerName] = state.ContainerState{
			ID:        containerID,
			PID:       inspect.PID,
			ConfigDir: prep.ConfigDir,
		}
	}

	if err := m.configureLinks(ctx, topology, snapshot); err != nil {
		return err
	}
	if err := m.configureLoopbacks(ctx, resolvedRouters, snapshot); err != nil {
		return err
	}
	if err := m.configureLinux(ctx, resolvedRouters, snapshot); err != nil {
		return err
	}
	if err := m.runRouterCommands(ctx, resolvedRouters, snapshot, prepared); err != nil {
		return err
	}
	if err := state.Write(topology.StatePath(), *snapshot); err != nil {
		return err
	}

	return nil
}

// Down removes the live runtime objects for topologyPath and optionally its
// generated state directory.
func (m *Manager) Down(ctx context.Context, topologyPath string, opts DownOptions) error {
	if err := requireHostNetworking(); err != nil {
		return err
	}

	topology, err := config.LoadFile(topologyPath)
	if err != nil {
		return err
	}

	return m.cleanupRuntime(ctx, topology, nil, opts.Purge)
}

// Console opens either vtysh or /bin/sh inside a running router container.
func (m *Manager) Console(ctx context.Context, topologyPath, router string, opts ConsoleOptions) error {
	containerID, err := m.lookupConsoleContainer(ctx, topologyPath, router)
	if err != nil {
		return err
	}

	command := []string{"vtysh"}
	if opts.Shell {
		command = []string{"/bin/sh"}
	}
	return m.docker.ExecInteractive(ctx, containerID, command)
}

// Ping runs one or all YAML-defined ping checks and returns the raw ping output
// for the CLI to print unchanged.
func (m *Manager) Ping(ctx context.Context, topologyPath string, name string) ([]PingResult, error) {
	topology, err := config.LoadFile(topologyPath)
	if err != nil {
		return nil, err
	}

	checks, err := selectPingChecks(topology.Pings, name)
	if err != nil {
		return nil, err
	}

	results := make([]PingResult, 0, len(checks))
	for _, check := range checks {
		containerID, err := m.lookupRouterContainer(ctx, topology, check.From.Router)
		if err != nil {
			return results, err
		}

		execResult, err := m.docker.Exec(ctx, containerID, pingCommand(check))
		if err != nil {
			return results, fmt.Errorf("ping %q failed to start: %w", check.Name, err)
		}

		results = append(results, PingResult{
			Name:      check.Name,
			Router:    check.From.Router,
			Namespace: check.From.Namespace,
			Target:    check.To,
			Output:    combineExecOutput(execResult),
			ExitCode:  execResult.ExitCode,
		})
	}

	return results, nil
}

func (m *Manager) runtimeExists(ctx context.Context, topology *config.Topology) (bool, error) {
	if _, err := os.Stat(topology.StatePath()); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat state file: %w", err)
	}

	containers, err := m.docker.ListContainers(ctx, map[string]string{
		labelManaged: "true",
		labelLab:     topology.Lab.Name,
	})
	if err != nil {
		return false, err
	}
	return len(containers) > 0, nil
}

func (m *Manager) configureLinks(ctx context.Context, topology *config.Topology, snapshot *state.LabState) error {
	for _, link := range topology.Links {
		switch link.Type {
		case "bridge":
			bridgeName := bridgeLinkName(topology.Lab.Name, link.Name)
			if err := m.network.EnsureBridge(bridgeName, link.MTU); err != nil {
				return err
			}
			for _, member := range link.Members {
				hostIfName := bridgePortName(topology.Lab.Name, link.Name, member.Router, member.IfName)
				peerIfName := bridgePeerName(topology.Lab.Name, link.Name, member.Router, member.IfName)
				container := snapshot.Containers[member.Router]
				if err := m.network.CreateBridgeAttachment(bridgeName, hostIfName, peerIfName, container.PID, link.MTU); err != nil {
					return err
				}
				if err := m.configureInterface(ctx, container.ID, peerIfName, member, link.MTU); err != nil {
					return err
				}
			}
		case "p2p":
			left := link.Members[0]
			right := link.Members[1]

			leftTemp := p2pTempName(topology.Lab.Name, link.Name, "a")
			rightTemp := p2pTempName(topology.Lab.Name, link.Name, "b")
			if err := m.network.CreateP2PAttachment(
				leftTemp,
				snapshot.Containers[left.Router].PID,
				rightTemp,
				snapshot.Containers[right.Router].PID,
				link.MTU,
			); err != nil {
				return err
			}
			if err := m.configureInterface(ctx, snapshot.Containers[left.Router].ID, leftTemp, left, link.MTU); err != nil {
				return err
			}
			if err := m.configureInterface(ctx, snapshot.Containers[right.Router].ID, rightTemp, right, link.MTU); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *Manager) configureLoopbacks(ctx context.Context, routers map[string]config.ResolvedRouter, snapshot *state.LabState) error {
	for _, routerName := range sortedResolvedRouterNames(routers) {
		container := snapshot.Containers[routerName]
		if err := m.runExec(ctx, container.ID, []string{"ip", "link", "set", "lo", "up"}, "bring loopback up"); err != nil {
			return fmt.Errorf("router %q: %w", routerName, err)
		}
		for _, loopback := range routers[routerName].Loopbacks {
			if err := m.runExec(ctx, container.ID, []string{"ip", "addr", "replace", loopback, "dev", "lo"}, "configure loopback"); err != nil {
				return fmt.Errorf("router %q: %w", routerName, err)
			}
		}
	}
	return nil
}

func (m *Manager) configureLinux(ctx context.Context, routers map[string]config.ResolvedRouter, snapshot *state.LabState) error {
	for _, routerName := range sortedResolvedRouterNames(routers) {
		container := snapshot.Containers[routerName]
		for _, vrf := range routers[routerName].Linux.VRFs {
			if err := m.configureLinuxVRF(ctx, container.ID, vrf); err != nil {
				return fmt.Errorf("router %q vrf %q: %w", routerName, vrf.Name, err)
			}
		}
		for _, bridge := range routers[routerName].Linux.Bridges {
			if err := m.configureLinuxBridge(ctx, container.ID, bridge); err != nil {
				return fmt.Errorf("router %q bridge %q: %w", routerName, bridge.Name, err)
			}
		}
		for _, iface := range routers[routerName].Linux.Interfaces {
			if err := m.configureLinuxInterface(ctx, container.ID, iface); err != nil {
				return fmt.Errorf("router %q interface %q: %w", routerName, iface.Name, err)
			}
		}
		for _, veth := range routers[routerName].Linux.Veths {
			if err := m.configureLinuxVeth(ctx, container.ID, veth); err != nil {
				return fmt.Errorf("router %q veth %q: %w", routerName, veth.Name, err)
			}
		}
		for _, route := range routers[routerName].Linux.Routes {
			if err := m.runExec(ctx, container.ID, routeCommand(route), "configure route"); err != nil {
				return fmt.Errorf("router %q route %q: %w", routerName, route.To, err)
			}
		}
	}
	return nil
}

func (m *Manager) configureLinuxVRF(ctx context.Context, containerID string, vrf config.VRF) error {
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "add", "name", vrf.Name, "type", "vrf", "table", strconv.Itoa(vrf.Table)}, "create vrf"); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", vrf.Name, "up"}, "bring vrf up"); err != nil {
		return err
	}
	return nil
}

func (m *Manager) configureLinuxBridge(ctx context.Context, containerID string, bridge config.Bridge) error {
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "add", "name", bridge.Name, "type", "bridge"}, "create bridge"); err != nil {
		return err
	}
	if err := m.configureLinkAttrs(ctx, containerID, bridge.Name, bridge.Master, bridge.MAC, bridge.AddrGenMode, bridge.Addresses); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", bridge.Name, "up"}, "bring bridge up"); err != nil {
		return err
	}
	for _, iface := range bridge.Interfaces {
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", iface, "master", bridge.Name}, "attach interface to bridge"); err != nil {
			return err
		}
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", iface, "up"}, "bring bridged interface up"); err != nil {
			return err
		}
	}
	for _, vxlan := range bridge.VXLANS {
		if err := m.runExec(ctx, containerID, vxlanCommand(vxlan), "create vxlan"); err != nil {
			return err
		}
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", vxlan.Name, "master", bridge.Name}, "attach vxlan to bridge"); err != nil {
			return err
		}
		if err := m.configureLinkAttrs(ctx, containerID, vxlan.Name, "", "", vxlan.AddrGenMode, nil); err != nil {
			return err
		}
		if command := bridgeSlaveCommand(vxlan); len(command) > 0 {
			if err := m.runExec(ctx, containerID, command, "configure bridge_slave vxlan"); err != nil {
				return err
			}
		}
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", vxlan.Name, "up"}, "bring vxlan up"); err != nil {
			return err
		}
	}
	for _, namespace := range bridge.Namespaces {
		if err := m.configureLinuxNamespace(ctx, containerID, bridge.Name, namespace); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) configureLinuxInterface(ctx context.Context, containerID string, iface config.Interface) error {
	if err := m.configureLinkAttrs(ctx, containerID, iface.Name, iface.Master, iface.MAC, iface.AddrGenMode, iface.Addresses); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", iface.Name, "up"}, "bring interface up"); err != nil {
		return err
	}
	return nil
}

func (m *Manager) configureLinuxVeth(ctx context.Context, containerID string, veth config.Veth) error {
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "add", "name", veth.Name, "type", "veth", "peer", "name", veth.Peer}, "create veth"); err != nil {
		return err
	}
	if err := m.configureLinkAttrs(ctx, containerID, veth.Name, veth.Master, veth.MAC, veth.AddrGenMode, veth.Addresses); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", veth.Name, "up"}, "bring veth up"); err != nil {
		return err
	}
	if veth.Namespace == nil {
		return nil
	}
	return m.configureLinuxNamespacePeer(ctx, containerID, veth.Peer, *veth.Namespace)
}

func (m *Manager) configureLinuxNamespace(ctx context.Context, containerID, bridgeName string, namespace config.Namespace) error {
	hostVeth := bridgeNamespaceHostVethName(bridgeName, namespace.Name)
	peerVeth := bridgeNamespacePeerVethName(bridgeName, namespace.Name)

	if err := m.runExec(ctx, containerID, []string{"ip", "link", "add", "name", hostVeth, "type", "veth", "peer", "name", peerVeth}, "create namespace veth"); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", hostVeth, "master", bridgeName}, "attach namespace veth to bridge"); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", hostVeth, "up"}, "bring namespace bridge veth up"); err != nil {
		return err
	}
	return m.configureLinuxNamespacePeer(ctx, containerID, peerVeth, namespace)
}

func (m *Manager) configureLinuxNamespacePeer(ctx context.Context, containerID, peerName string, namespace config.Namespace) error {
	if err := m.runExec(ctx, containerID, []string{"ip", "netns", "add", namespace.Name}, "create namespace"); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", peerName, "netns", namespace.Name}, "move namespace veth"); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "netns", "exec", namespace.Name, "ip", "link", "set", "dev", "lo", "up"}, "bring namespace loopback up"); err != nil {
		return err
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "netns", "exec", namespace.Name, "ip", "link", "set", "dev", peerName, "name", namespace.IfName}, "rename namespace interface"); err != nil {
		return err
	}
	if namespace.MAC != "" {
		if err := m.runExec(ctx, containerID, []string{"ip", "netns", "exec", namespace.Name, "ip", "link", "set", "dev", namespace.IfName, "address", namespace.MAC}, "set namespace interface mac"); err != nil {
			return err
		}
	}
	for _, address := range namespace.Addresses {
		if err := m.runExec(ctx, containerID, []string{"ip", "netns", "exec", namespace.Name, "ip", "addr", "replace", address, "dev", namespace.IfName}, "configure namespace address"); err != nil {
			return err
		}
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "netns", "exec", namespace.Name, "ip", "link", "set", "dev", namespace.IfName, "up"}, "bring namespace interface up"); err != nil {
		return err
	}
	if namespace.DefaultVia != "" {
		if err := m.runExec(ctx, containerID, []string{"ip", "netns", "exec", namespace.Name, "ip", "route", "replace", "default", "via", namespace.DefaultVia}, "configure namespace default route"); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) configureLinkAttrs(ctx context.Context, containerID, ifName, master, mac, addrGenMode string, addresses []string) error {
	if strings.TrimSpace(master) != "" {
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", ifName, "master", master}, "attach interface to master"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(addrGenMode) != "" {
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", ifName, "addrgenmode", addrGenMode}, "set interface addrgenmode"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(mac) != "" {
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", ifName, "address", mac}, "set interface mac"); err != nil {
			return err
		}
	}
	for _, address := range addresses {
		if err := m.runExec(ctx, containerID, []string{"ip", "addr", "replace", address, "dev", ifName}, "configure interface address"); err != nil {
			return err
		}
	}
	return nil
}

// runRouterCommands executes startup commands in YAML order. Shell commands
// always run; vtysh commands run only on the first boot or an explicit reseed.
func (m *Manager) runRouterCommands(ctx context.Context, routers map[string]config.ResolvedRouter, snapshot *state.LabState, prepared map[string]frr.PrepareResult) error {
	for _, routerName := range sortedResolvedRouterNames(routers) {
		container := snapshot.Containers[routerName]
		waitedForVTYSH := false
		ranVTYSH := false

		for _, command := range routers[routerName].Commands {
			switch command.Kind {
			case "shell":
				result, err := m.docker.Exec(ctx, container.ID, []string{"sh", "-lc", command.Run})
				if err != nil {
					return fmt.Errorf("router %q shell command failed: %w", routerName, err)
				}
				if result.ExitCode != 0 {
					return fmt.Errorf("router %q shell command failed with exit code %d: %s", routerName, result.ExitCode, strings.TrimSpace(result.Stderr))
				}
			case "vtysh":
				if !prepared[routerName].NeedsSeed {
					continue
				}
				if !waitedForVTYSH {
					if err := m.waitForVTYSH(ctx, container.ID, frr.EnabledDaemons()); err != nil {
						return fmt.Errorf("router %q: %w", routerName, err)
					}
					waitedForVTYSH = true
				}

				result, err := m.docker.Exec(ctx, container.ID, []string{"sh", "-lc", renderVTYSH(command.Run)})
				if err != nil {
					return fmt.Errorf("router %q vtysh seed failed: %w", routerName, err)
				}
				if result.ExitCode != 0 {
					return fmt.Errorf("router %q vtysh seed failed with exit code %d: %s", routerName, result.ExitCode, strings.TrimSpace(result.Stderr))
				}
				if message, failed := vtyshWriteFailure(result); failed {
					return fmt.Errorf("router %q vtysh seed failed: %s", routerName, message)
				}
				ranVTYSH = true
			}
		}

		if ranVTYSH {
			if err := frr.MarkSeeded(prepared[routerName].MarkerPath); err != nil {
				return fmt.Errorf("router %q: %w", routerName, err)
			}
		}
	}
	return nil
}

// cleanupRuntime removes every known container and deterministic host-side
// link name for a lab so retries start from a clean slate even after a partial
// failure.
func (m *Manager) cleanupRuntime(ctx context.Context, topology *config.Topology, snapshot *state.LabState, purge bool) error {
	var cleanupErr error

	ids := make(map[string]struct{})
	if snapshot == nil {
		if loaded, err := state.Read(topology.StatePath()); err == nil {
			snapshot = loaded
		}
	}
	if snapshot != nil {
		for _, container := range snapshot.Containers {
			ids[container.ID] = struct{}{}
		}
	}

	labeledContainers, err := m.docker.ListContainers(ctx, map[string]string{
		labelManaged: "true",
		labelLab:     topology.Lab.Name,
	})
	if err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	} else {
		for _, id := range labeledContainers {
			ids[id] = struct{}{}
		}
	}

	for id := range ids {
		if err := m.docker.RemoveContainer(ctx, id); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}

	for _, link := range topology.Links {
		switch link.Type {
		case "bridge":
			for _, member := range link.Members {
				if err := m.network.DeleteLink(bridgePortName(topology.Lab.Name, link.Name, member.Router, member.IfName)); err != nil {
					cleanupErr = errors.Join(cleanupErr, err)
				}
				if err := m.network.DeleteLink(bridgePeerName(topology.Lab.Name, link.Name, member.Router, member.IfName)); err != nil {
					cleanupErr = errors.Join(cleanupErr, err)
				}
			}
			if err := m.network.DeleteLink(bridgeLinkName(topology.Lab.Name, link.Name)); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		case "p2p":
			if err := m.network.DeleteLink(p2pTempName(topology.Lab.Name, link.Name, "a")); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
			if err := m.network.DeleteLink(p2pTempName(topology.Lab.Name, link.Name, "b")); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
	}

	if err := state.Remove(topology.StatePath()); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}

	if purge {
		if err := os.RemoveAll(topology.LabDir()); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("purge lab dir: %w", err))
		}
	}

	return cleanupErr
}

func (m *Manager) lookupConsoleContainer(ctx context.Context, topologyPath, router string) (string, error) {
	if strings.TrimSpace(topologyPath) != "" {
		topology, err := config.LoadFile(topologyPath)
		if err != nil {
			return "", err
		}
		return m.lookupRouterContainer(ctx, topology, router)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}

	snapshots, err := state.ScanWorkDir(config.ResolveWorkDir(cwd, ""))
	if err != nil {
		return "", err
	}

	var matches []string
	for _, snapshot := range snapshots {
		if container, ok := snapshot.State.Containers[router]; ok {
			matches = append(matches, container.ID)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("router %q not found; pass --file when using a non-default workdir", router)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("router %q exists in multiple labs; pass --file to disambiguate", router)
	}
	return matches[0], nil
}

func (m *Manager) lookupRouterContainer(ctx context.Context, topology *config.Topology, router string) (string, error) {
	if snapshot, err := state.Read(topology.StatePath()); err == nil {
		if container, ok := snapshot.Containers[router]; ok {
			return container.ID, nil
		}
	}

	containers, err := m.docker.ListContainers(ctx, map[string]string{
		labelManaged: "true",
		labelLab:     topology.Lab.Name,
		labelRouter:  router,
	})
	if err != nil {
		return "", err
	}
	if len(containers) == 0 {
		return "", fmt.Errorf("router %q is not running in lab %q", router, topology.Lab.Name)
	}
	if len(containers) > 1 {
		return "", fmt.Errorf("router %q is ambiguous in lab %q", router, topology.Lab.Name)
	}
	return containers[0], nil
}

func (m *Manager) configureInterface(ctx context.Context, containerID, tempIfName string, member config.LinkMember, mtu int) error {
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", tempIfName, "name", member.IfName}, "rename interface"); err != nil {
		return err
	}
	if mtu > 0 {
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", member.IfName, "mtu", strconv.Itoa(mtu)}, "set interface mtu"); err != nil {
			return err
		}
	}
	if member.MAC != "" {
		if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", member.IfName, "address", member.MAC}, "set interface mac"); err != nil {
			return err
		}
	}
	if member.IPv4 != "" {
		if err := m.runExec(ctx, containerID, []string{"ip", "addr", "replace", member.IPv4, "dev", member.IfName}, "set interface address"); err != nil {
			return err
		}
	}
	if err := m.runExec(ctx, containerID, []string{"ip", "link", "set", "dev", member.IfName, "up"}, "bring interface up"); err != nil {
		return err
	}
	return nil
}

// waitForVTYSH waits until vtysh can see every enabled daemon, which makes
// subsequent seed commands and write-memory calls deterministic.
func (m *Manager) waitForVTYSH(ctx context.Context, containerID string, daemons []string) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		result, err := m.docker.Exec(ctx, containerID, []string{"vtysh", "-c", "show daemons"})
		if err == nil && result.ExitCode == 0 {
			running := strings.Fields(result.Stdout)
			if hasDaemons(running, daemons) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("wait for vtysh: %w", err)
			}
			message := strings.TrimSpace(result.Stderr)
			if message == "" {
				message = strings.TrimSpace(result.Stdout)
			}
			return fmt.Errorf("wait for vtysh timed out: %s", message)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func hasDaemons(running, want []string) bool {
	for _, daemon := range want {
		if !slices.Contains(running, daemon) {
			return false
		}
	}
	return true
}

// renderVTYSH uses a quoted heredoc so the user's multiline config is passed to
// vtysh unchanged, including leading spaces on indented FRR submodes.
func renderVTYSH(configText string) string {
	return "cat <<'EOF' | vtysh\n" + strings.TrimSpace(configText) + "\nend\nwrite memory\nEOF\n"
}

// vtyshWriteFailure catches the write-memory failures that FRR reports in
// output text while still exiting successfully.
func vtyshWriteFailure(result docker.ExecResult) (string, bool) {
	output := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	for _, marker := range []string{
		"% Configuration write failed.",
		"% Not all daemons are up, cannot write config.",
	} {
		if strings.Contains(output, marker) {
			return strings.TrimSpace(output), true
		}
	}
	return "", false
}

func selectPingChecks(checks []config.Ping, name string) ([]config.Ping, error) {
	if len(checks) == 0 {
		return nil, fmt.Errorf("topology does not define any pings")
	}
	if strings.TrimSpace(name) == "" {
		return append([]config.Ping(nil), checks...), nil
	}

	for _, check := range checks {
		if check.Name == name {
			return []config.Ping{check}, nil
		}
	}
	return nil, fmt.Errorf("ping %q not found in topology", name)
}

func routeCommand(route config.Route) []string {
	command := []string{"ip", "route", "replace", route.To}
	if strings.TrimSpace(route.Via) != "" {
		command = append(command, "via", route.Via)
	}
	if strings.TrimSpace(route.Dev) != "" {
		command = append(command, "dev", route.Dev)
	}
	return command
}

func vxlanCommand(vxlan config.VXLAN) []string {
	command := []string{
		"ip", "link", "add", "name", vxlan.Name,
		"type", "vxlan",
		"id", strconv.Itoa(vxlan.VNI),
	}
	if strings.TrimSpace(vxlan.Local) != "" {
		command = append(command, "local", vxlan.Local)
	}
	dstPort := vxlan.DstPort
	if dstPort == 0 {
		dstPort = 4789
	}
	command = append(command, "dstport", strconv.Itoa(dstPort))
	if vxlan.NoLearning {
		command = append(command, "nolearning")
	}
	return command
}

func bridgeSlaveCommand(vxlan config.VXLAN) []string {
	if vxlan.BridgeSlave.NeighSuppress == nil && vxlan.BridgeSlave.Learning == nil {
		return nil
	}

	command := []string{"ip", "link", "set", "dev", vxlan.Name, "type", "bridge_slave"}
	if vxlan.BridgeSlave.NeighSuppress != nil {
		command = append(command, "neigh_suppress", onOff(*vxlan.BridgeSlave.NeighSuppress))
	}
	if vxlan.BridgeSlave.Learning != nil {
		command = append(command, "learning", onOff(*vxlan.BridgeSlave.Learning))
	}
	return command
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func pingCommand(check config.Ping) []string {
	count := check.Count
	if count == 0 {
		count = 3
	}

	command := []string{"ping", "-c", strconv.Itoa(count), "-W", "1"}
	if source := strings.TrimSpace(check.From.Address); source != "" {
		command = append(command, "-I", source)
	}
	command = append(command, check.To)
	if strings.TrimSpace(check.From.Namespace) == "" {
		return command
	}

	return append([]string{"ip", "netns", "exec", check.From.Namespace}, command...)
}

func combineExecOutput(result docker.ExecResult) string {
	if result.Stderr == "" {
		return result.Stdout
	}
	if result.Stdout == "" {
		return result.Stderr
	}
	return result.Stdout + result.Stderr
}

func (m *Manager) runExec(ctx context.Context, containerID string, cmd []string, operation string) error {
	result, err := m.docker.Exec(ctx, containerID, cmd)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = strings.TrimSpace(result.Stdout)
		}
		return fmt.Errorf("%s failed with exit code %d: %s", operation, result.ExitCode, message)
	}
	return nil
}

func requireHostNetworking() error {
	if goruntime.GOOS != "linux" {
		return fmt.Errorf("frridge requires Linux for host-side network namespace management")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("frridge up/down require root to create host bridges and veth pairs")
	}
	return nil
}

func routerLabels(labName, routerName, digest string) map[string]string {
	return map[string]string{
		labelManaged:  "true",
		labelLab:      labName,
		labelRouter:   routerName,
		labelTopology: digest,
	}
}

func toDockerMounts(mounts []config.Mount) []docker.Mount {
	if len(mounts) == 0 {
		return nil
	}
	output := make([]docker.Mount, 0, len(mounts))
	for _, mount := range mounts {
		output = append(output, docker.Mount{
			Source:   mount.Source,
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
	}
	return output
}

func sortedRouterNames(routers map[string]config.Router) []string {
	names := make([]string, 0, len(routers))
	for name := range routers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedResolvedRouterNames(routers map[string]config.ResolvedRouter) []string {
	names := make([]string, 0, len(routers))
	for name := range routers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func containerName(labName, routerName string) string {
	return sanitizeName("frridge-" + labName + "-" + routerName)
}

func sanitizeName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

// shortName keeps host-side object names inside Linux's 15-byte interface limit
// while remaining deterministic across up/down cycles.
func shortName(prefix, key string) string {
	digest := sha1hex(key)
	return prefix + digest[:11]
}

func bridgeLinkName(labName, linkName string) string {
	return shortName("frb", labName+"/"+linkName)
}

func bridgePortName(labName, linkName, routerName, ifName string) string {
	return shortName("frh", labName+"/"+linkName+"/"+routerName+"/"+ifName+"/host")
}

func bridgePeerName(labName, linkName, routerName, ifName string) string {
	return shortName("frp", labName+"/"+linkName+"/"+routerName+"/"+ifName+"/peer")
}

func bridgeNamespaceHostVethName(bridgeName, namespaceName string) string {
	return shortName("fnh", bridgeName+"/"+namespaceName+"/host")
}

func bridgeNamespacePeerVethName(bridgeName, namespaceName string) string {
	return shortName("fnp", bridgeName+"/"+namespaceName+"/peer")
}

func p2pTempName(labName, linkName, side string) string {
	return shortName("frt", labName+"/"+linkName+"/"+side)
}

func sha1hex(value string) string {
	sum := sha1.Sum([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}
