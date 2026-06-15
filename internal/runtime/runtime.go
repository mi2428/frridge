// Package runtime orchestrates Docker containers, host networking, and FRR
// bootstrap files into a runnable lab.
package runtime

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"frridge/internal/config"
	"frridge/internal/docker"
	"frridge/internal/frr"
	"frridge/internal/netutil"
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

// Manager implements Service by coordinating Docker, netlink, and generated
// FRR config files.
type Manager struct {
	docker  docker.Client
	network netutil.Manager
}

// NewManager builds a runtime manager from explicit Docker and networking
// dependencies.
func NewManager(dockerClient docker.Client, networkManager netutil.Manager) *Manager {
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
	return NewManager(dockerClient, netutil.New()), nil
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
	if err := m.runShellCommands(ctx, resolvedRouters, snapshot); err != nil {
		return err
	}
	if err := m.seedRouters(ctx, resolvedRouters, snapshot, prepared); err != nil {
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

func (m *Manager) runShellCommands(ctx context.Context, routers map[string]config.ResolvedRouter, snapshot *state.LabState) error {
	for _, routerName := range sortedResolvedRouterNames(routers) {
		container := snapshot.Containers[routerName]
		for _, command := range routers[routerName].Commands {
			if command.Kind != "shell" {
				continue
			}
			result, err := m.docker.Exec(ctx, container.ID, []string{"sh", "-lc", command.Run})
			if err != nil {
				return fmt.Errorf("router %q shell command failed: %w", routerName, err)
			}
			if result.ExitCode != 0 {
				return fmt.Errorf("router %q shell command failed with exit code %d: %s", routerName, result.ExitCode, strings.TrimSpace(result.Stderr))
			}
		}
	}
	return nil
}

func (m *Manager) seedRouters(ctx context.Context, routers map[string]config.ResolvedRouter, snapshot *state.LabState, prepared map[string]frr.PrepareResult) error {
	for _, routerName := range sortedResolvedRouterNames(routers) {
		if !prepared[routerName].NeedsSeed {
			continue
		}

		container := snapshot.Containers[routerName]
		commands := make([]config.Command, 0)
		for _, command := range routers[routerName].Commands {
			if command.Kind == "vtysh" {
				commands = append(commands, command)
			}
		}
		if len(commands) == 0 {
			continue
		}

		if err := m.waitForVTYSH(ctx, container.ID); err != nil {
			return fmt.Errorf("router %q: %w", routerName, err)
		}

		for _, command := range commands {
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
		}
		if err := frr.MarkSeeded(prepared[routerName].MarkerPath); err != nil {
			return fmt.Errorf("router %q: %w", routerName, err)
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

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}

	snapshots, err := state.ScanWorkDir(filepath.Join(cwd, config.DefaultWorkDir))
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

// waitForVTYSH waits until vtysh can see the daemons that the generated FRR
// files always enable, which makes subsequent seed commands and write-memory
// calls deterministic.
func (m *Manager) waitForVTYSH(ctx context.Context, containerID string) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		result, err := m.docker.Exec(ctx, containerID, []string{"vtysh", "-c", "show daemons"})
		if err == nil && result.ExitCode == 0 {
			daemons := strings.Fields(result.Stdout)
			if slices.Contains(daemons, "zebra") && slices.Contains(daemons, "bgpd") {
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

func p2pTempName(labName, linkName, side string) string {
	return shortName("frt", labName+"/"+linkName+"/"+side)
}

func sha1hex(value string) string {
	sum := sha1.Sum([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}
