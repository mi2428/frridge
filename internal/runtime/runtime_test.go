package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"frridge/internal/config"
	"frridge/internal/docker"
	"frridge/internal/frr"
	"frridge/internal/state"
)

func TestShortNamesFitLinuxInterfaceLimit(t *testing.T) {
	t.Parallel()
	if got, want := bridgeLinkName("fabric", "lan-a"), shortName("frb", "fabric/lan-a"); got != want {
		t.Fatalf("bridge naming changed from the legacy algorithm: got=%q want=%q", got, want)
	}

	names := []string{
		bridgeLinkName("very-long-lab-name", "very-long-link-name"),
		bridgePortName("very-long-lab-name", "very-long-link-name", "router-a", "eth999"),
		bridgePeerName("very-long-lab-name", "very-long-link-name", "router-a", "eth999"),
		p2pTempName("very-long-lab-name", "very-long-link-name", "side-a"),
	}

	for _, name := range names {
		if len(name) > 15 {
			t.Fatalf("name %q has len=%d, want <=15", name, len(name))
		}
	}
}

type fakeDockerClient struct {
	daemons   []string
	execs     [][]string
	execFn    func(context.Context, string, []string) (docker.ExecResult, error)
	listErr   error
	removeErr error
	removed   []string
	startFn   func(context.Context, string) error
	inspectFn func(context.Context, string) (docker.Inspect, error)
	listFn    func(context.Context, map[string]string) ([]string, error)
	removeFn  func(context.Context, string) error
}

type fakeNetworkManager struct {
	ops       [][]string
	deleteErr error
	ensureFn  func(string, int) error
}

func (f *fakeDockerClient) CreateContainer(_ context.Context, spec docker.ContainerSpec) (string, error) {
	return spec.Name, nil
}

func (f *fakeDockerClient) StartContainer(ctx context.Context, id string) error {
	if f.startFn != nil {
		return f.startFn(ctx, id)
	}
	return nil
}

func (f *fakeDockerClient) InspectContainer(ctx context.Context, id string) (docker.Inspect, error) {
	if f.inspectFn != nil {
		return f.inspectFn(ctx, id)
	}
	return docker.Inspect{ID: id, PID: 101}, nil
}

func (f *fakeDockerClient) ListContainers(ctx context.Context, labels map[string]string) ([]string, error) {
	if f.listFn != nil {
		return f.listFn(ctx, labels)
	}
	return nil, f.listErr
}

func (f *fakeDockerClient) RemoveContainer(ctx context.Context, id string) error {
	f.removed = append(f.removed, id)
	if f.removeFn != nil {
		return f.removeFn(ctx, id)
	}
	return f.removeErr
}

func (f *fakeDockerClient) Exec(ctx context.Context, containerID string, cmd []string) (docker.ExecResult, error) {
	if f.execFn != nil {
		return f.execFn(ctx, containerID, cmd)
	}
	if len(cmd) == 3 && cmd[0] == "vtysh" && cmd[1] == "-c" && cmd[2] == "show daemons" {
		return docker.ExecResult{ExitCode: 0, Stdout: strings.Join(f.daemons, " ")}, nil
	}
	f.execs = append(f.execs, append([]string(nil), cmd...))
	return docker.ExecResult{ExitCode: 0}, nil
}

func (f *fakeDockerClient) ExecInteractive(context.Context, string, []string) error {
	return nil
}

func (f *fakeNetworkManager) record(op ...string) {
	f.ops = append(f.ops, append([]string(nil), op...))
}

func (f *fakeNetworkManager) EnsureBridge(name string, mtu int) error {
	f.record("ensure-bridge", name, strconv.Itoa(mtu))
	if f.ensureFn != nil {
		return f.ensureFn(name, mtu)
	}
	return nil
}

func (f *fakeNetworkManager) CreateBridgeAttachment(bridgeName, hostIfName, peerIfName string, peerPID, mtu int) error {
	f.record("create-bridge-attachment", bridgeName, hostIfName, peerIfName, strconv.Itoa(peerPID), strconv.Itoa(mtu))
	return nil
}

func (f *fakeNetworkManager) CreateP2PAttachment(aIfName string, aPID int, bIfName string, bPID int, mtu int) error {
	f.record("create-p2p-attachment", aIfName, strconv.Itoa(aPID), bIfName, strconv.Itoa(bPID), strconv.Itoa(mtu))
	return nil
}

func (f *fakeNetworkManager) ConfigureAttachedLink(pid int, tempIfName string, member config.LinkMember, mtu int) error {
	f.record("configure-attached-link", strconv.Itoa(pid), tempIfName, member.IfName, member.IPv4, member.MAC, strconv.Itoa(mtu))
	return nil
}

func (f *fakeNetworkManager) ConfigureLoopback(pid int, addresses []string) error {
	f.record(append([]string{"configure-loopback", strconv.Itoa(pid)}, addresses...)...)
	return nil
}

func (f *fakeNetworkManager) ConfigureVRF(pid int, vrf config.VRF) error {
	f.record("configure-vrf", strconv.Itoa(pid), vrf.Name, strconv.Itoa(vrf.Table))
	return nil
}

func (f *fakeNetworkManager) ConfigureBond(pid int, bond config.Bond) error {
	f.record(
		"configure-bond",
		strconv.Itoa(pid),
		bond.Name,
		bond.Mode,
		bond.Master,
		strings.Join(bond.Interfaces, ","),
		bond.MAC,
		bond.AddrGenMode,
		strings.Join(bond.Addresses, ","),
	)
	return nil
}

func (f *fakeNetworkManager) ConfigureBridge(pid int, bridge config.Bridge) error {
	f.record(
		"configure-bridge",
		strconv.Itoa(pid),
		bridge.Name,
		bridge.Master,
		bridge.MAC,
		bridge.AddrGenMode,
		strings.Join(bridge.Interfaces, ","),
		summarizeVXLANS(bridge.VXLANS),
	)
	return nil
}

func (f *fakeNetworkManager) AttachLinkMaster(pid int, ifName, master string) error {
	f.record("attach-link-master", strconv.Itoa(pid), ifName, master)
	return nil
}

func (f *fakeNetworkManager) ConfigureInterface(pid int, iface config.Interface) error {
	f.record(
		"configure-interface",
		strconv.Itoa(pid),
		iface.Name,
		iface.Master,
		iface.MAC,
		iface.AddrGenMode,
		strings.Join(iface.Addresses, ","),
	)
	return nil
}

func (f *fakeNetworkManager) ConfigureVeth(pid int, veth config.Veth) error {
	hasNamespace := "false"
	if veth.Namespace != nil {
		hasNamespace = "true"
	}
	f.record(
		"configure-veth",
		strconv.Itoa(pid),
		veth.Name,
		veth.Peer,
		veth.Master,
		veth.MAC,
		veth.AddrGenMode,
		strings.Join(veth.Addresses, ","),
		hasNamespace,
	)
	return nil
}

func (f *fakeNetworkManager) ConfigureBridgeNamespaceVeth(pid int, bridgeName, hostVeth, peerVeth string) error {
	f.record("configure-bridge-namespace-veth", strconv.Itoa(pid), bridgeName, hostVeth, peerVeth)
	return nil
}

func (f *fakeNetworkManager) ConfigureRoute(pid int, route config.Route) error {
	f.record("configure-route", strconv.Itoa(pid), route.To, route.Via, route.Dev)
	return nil
}

func (f *fakeNetworkManager) DeleteLink(name string) error {
	f.record("delete-link", name)
	return f.deleteErr
}

func TestCleanupRuntimePreservesArtifactsOnFailureAndAllowsRetry(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"list", "remove", "network", "none"} {
		for _, purge := range []bool{false, true} {
			t.Run(failure+"/purge="+strconv.FormatBool(purge), func(t *testing.T) {
				topology := &config.Topology{
					Lab:   config.Lab{Name: "cleanup", WorkDir: t.TempDir()},
					Links: []config.Link{{Name: "lan", Type: "bridge"}},
				}
				configPath := filepath.Join(topology.RouterDir("r1"), "frr.conf")
				if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configPath, []byte("hostname r1\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := state.Write(topology.StatePath(), state.LabState{
					Lab:            topology.Lab.Name,
					TopologyDigest: "sha256:" + strings.Repeat("0", 64),
					Containers:     map[string]state.ContainerState{"r1": {ID: "container-1"}},
					OwnedBridges:   map[string]string{"lan": bridgeLinkName(topology.Lab.Name, "lan")},
				}); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(topology.StatePath())
				if err != nil {
					t.Fatal(err)
				}
				failureErr := errors.New("injected cleanup failure")
				fakeDocker := &fakeDockerClient{}
				fakeNetwork := &fakeNetworkManager{}
				switch failure {
				case "list":
					fakeDocker.listErr = failureErr
				case "remove":
					fakeDocker.removeErr = failureErr
				case "network":
					fakeNetwork.deleteErr = failureErr
				}
				manager := NewManager(fakeDocker, fakeNetwork)
				topology.Links[0].Name = "renamed"
				if failure != "none" {
					if err := manager.cleanupRuntime(context.Background(), topology, nil, purge); !errors.Is(err, failureErr) {
						t.Fatalf("cleanup error = %v, want injected failure", err)
					}
					after, err := os.ReadFile(topology.StatePath())
					if err != nil || string(after) != string(before) {
						t.Fatalf("snapshot changed after failure: %q, %v", after, err)
					}
					if data, err := os.ReadFile(configPath); err != nil || string(data) != "hostname r1\n" {
						t.Fatalf("configuration changed after failure: %q, %v", data, err)
					}
					if len(fakeDocker.removed) == 0 || len(fakeNetwork.ops) == 0 {
						t.Fatal("cleanup did not continue attempting both resource types")
					}
				}
				fakeDocker.listErr, fakeDocker.removeErr, fakeNetwork.deleteErr = nil, nil, nil
				if err := manager.cleanupRuntime(context.Background(), topology, nil, purge); err != nil {
					t.Fatalf("successful cleanup error = %v", err)
				}
				if !hasDeletedLink(fakeNetwork, bridgeLinkName(topology.Lab.Name, "lan")) || hasDeletedLink(fakeNetwork, bridgeLinkName(topology.Lab.Name, "renamed")) {
					t.Fatal("failed-cleanup retry did not retain the original owned bridge")
				}
				if _, err := os.Stat(topology.StatePath()); !os.IsNotExist(err) {
					t.Fatalf("state remains after successful cleanup: %v", err)
				}
				_, err = os.Stat(configPath)
				if (purge && !os.IsNotExist(err)) || (!purge && err != nil) {
					t.Fatalf("configuration after purge=%t: %v", purge, err)
				}
			})
		}
	}
}

func startupTopologyFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.yaml")
	yaml := fmt.Sprintf(`apiVersion: frridge/v1alpha1
lab:
  name: startup
  workdir: %q
routers:
  r1: {}
  r2: {}
links:
  - name: lan
    type: bridge
    members:
      - router: r1
        ifname: eth1
      - router: r2
        ifname: eth1
`, filepath.Join(dir, "state"))
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testInterruptedStartupRollback(t *testing.T, native bool) {
	t.Helper()
	for _, stage := range []string{"start", "inspect", "links"} {
		for _, failure := range []string{"none", "list", "remove", "network"} {
			t.Run(stage+"/rollback="+failure, func(t *testing.T) {
				path := startupTopologyFile(t)
				type contextKey struct{}
				ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "startup-value"))
				defer cancel()
				cleanupFailure := errors.New("injected rollback failure")
				checkCleanupContext := func(cleanupCtx context.Context) {
					t.Helper()
					deadline, bounded := cleanupCtx.Deadline()
					remaining := time.Until(deadline)
					if cleanupCtx.Err() != nil || !bounded || remaining <= 0 || remaining > 30*time.Second || cleanupCtx.Value(contextKey{}) != "startup-value" {
						t.Fatalf("rollback context: err=%v, bounded=%t, remaining=%v, value=%v", cleanupCtx.Err(), bounded, remaining, cleanupCtx.Value(contextKey{}))
					}
				}
				fakeDocker := &fakeDockerClient{}
				fakeDocker.startFn = func(startCtx context.Context, _ string) error {
					if stage == "start" {
						cancel()
						return startCtx.Err()
					}
					return nil
				}
				fakeDocker.inspectFn = func(inspectCtx context.Context, id string) (docker.Inspect, error) {
					if stage == "inspect" {
						cancel()
						return docker.Inspect{}, inspectCtx.Err()
					}
					return docker.Inspect{ID: id, PID: 101}, nil
				}
				lists := 0
				fakeDocker.listFn = func(listCtx context.Context, labels map[string]string) ([]string, error) {
					lists++
					if labels[labelLab] != "startup" || labels[labelManaged] != "true" {
						t.Fatalf("cleanup discovery labels = %v", labels)
					}
					if lists == 1 {
						return nil, nil
					}
					checkCleanupContext(listCtx)
					if failure == "list" {
						return nil, cleanupFailure
					}
					return nil, nil
				}
				fakeDocker.removeFn = func(removeCtx context.Context, _ string) error {
					checkCleanupContext(removeCtx)
					if failure == "remove" {
						return cleanupFailure
					}
					return nil
				}
				fakeNetwork := &fakeNetworkManager{}
				if stage == "links" {
					fakeNetwork.ensureFn = func(string, int) error { cancel(); return nil }
				}
				if failure == "network" {
					fakeNetwork.deleteErr = cleanupFailure
				}
				manager := NewManager(fakeDocker, fakeNetwork)
				up := manager.up
				if native {
					up = manager.Up
				}
				err := up(ctx, path, UpOptions{})
				if !errors.Is(err, context.Canceled) || (failure != "none" && !errors.Is(err, cleanupFailure)) {
					t.Fatalf("startup error = %v, want original cancellation and rollback=%s", err, failure)
				}
				wantContainers := 1
				if stage == "links" {
					wantContainers = 2
				}
				if len(fakeDocker.removed) != wantContainers || lists != 2 {
					t.Fatalf("created ID not recovered before start/inspect: removed=%v, lists=%d", fakeDocker.removed, lists)
				}
				for _, id := range fakeDocker.removed {
					if id != containerName("startup", "r1") && (stage != "links" || id != containerName("startup", "r2")) {
						t.Fatalf("rollback removed an unknown ID %q", id)
					}
				}
				if len(fakeNetwork.ops) == 0 {
					t.Fatal("rollback skipped networking after cancellation")
				}
				topology, err := config.LoadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(topology.RouterConfigDir("r1"), "frr.conf")); err != nil {
					t.Fatalf("rollback lost generated FRR configuration: %v", err)
				}
			})
		}
	}
}

func TestInterruptedStartupRollbackWithFakes(t *testing.T) {
	t.Parallel()
	testInterruptedStartupRollback(t, false)
}

func TestNativeUpInterruptedRollbackWithFakes(t *testing.T) {
	if err := requireHostNetworking(); err != nil {
		t.Skipf("native privilege-gated startup unsupported: %v", err)
	}
	testInterruptedStartupRollback(t, true)
}

func TestStartupSuccessAndRecreateWithFakes(t *testing.T) {
	t.Parallel()
	path := startupTopologyFile(t)
	fakeDocker := &fakeDockerClient{}
	fakeNetwork := &fakeNetworkManager{}
	manager := NewManager(fakeDocker, fakeNetwork)
	for _, recreate := range []bool{false, true} {
		if err := manager.up(context.Background(), path, UpOptions{Recreate: recreate}); err != nil {
			t.Fatalf("successful startup recreate=%t: %v", recreate, err)
		}
		topology, err := config.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := state.Read(topology.StatePath())
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Containers) != 2 || len(fakeNetwork.ops) == 0 {
			t.Fatalf("successful startup snapshot=%+v, network ops=%v", snapshot, fakeNetwork.ops)
		}
		for router, container := range snapshot.Containers {
			if container.ID != containerName("startup", router) || container.PID != 101 || container.ConfigDir != topology.RouterConfigDir(router) {
				t.Fatalf("successful startup container=%+v", container)
			}
		}
		wantRemoved := 0
		if recreate {
			wantRemoved = 2
		}
		if len(fakeDocker.removed) != wantRemoved {
			t.Fatalf("success incorrectly ran rollback: removed=%v, want count=%d", fakeDocker.removed, wantRemoved)
		}
	}
}

func TestRenderVTYSHAppendsWriteMemory(t *testing.T) {
	t.Parallel()

	script := renderVTYSH(`
configure terminal
hostname leaf1
`)
	if !strings.Contains(script, "write memory") {
		t.Fatalf("renderVTYSH() = %q, want write memory", script)
	}
	if !strings.Contains(script, "end\nwrite memory") {
		t.Fatalf("renderVTYSH() = %q, want end before write memory", script)
	}
}

func TestVTYSHWriteFailureDetectsWriteMemoryWarnings(t *testing.T) {
	t.Parallel()

	message, failed := vtyshWriteFailure(docker.ExecResult{
		Stdout: "Note: this version of vtysh never writes vtysh.conf\n% Not all daemons are up, cannot write config.\n",
	})
	if !failed {
		t.Fatalf("vtyshWriteFailure() = false, want true")
	}
	if !strings.Contains(message, "cannot write config") {
		t.Fatalf("vtyshWriteFailure() message = %q, want write failure", message)
	}
}

func TestSelectPingChecksFiltersByName(t *testing.T) {
	t.Parallel()

	checks, err := selectPingChecks([]config.Ping{
		{Name: "one"},
		{Name: "two"},
	}, "two")
	if err != nil {
		t.Fatalf("selectPingChecks() error = %v", err)
	}
	if len(checks) != 1 || checks[0].Name != "two" {
		t.Fatalf("selectPingChecks() = %#v, want only named check", checks)
	}
}

func TestPingCommandSupportsOptionalNamespace(t *testing.T) {
	t.Parallel()

	withNamespace := pingCommand(config.Ping{
		From: config.PingSource{Namespace: "host"},
		To:   "10.0.0.1",
	})
	if got, want := strings.Join(withNamespace, "\x00"), strings.Join([]string{"ip", "netns", "exec", "host", "ping", "-c", "3", "-W", "1", "10.0.0.1"}, "\x00"); got != want {
		t.Fatalf("pingCommand() with namespace = %#v, want %#v", withNamespace, []string{"ip", "netns", "exec", "host", "ping", "-c", "3", "-W", "1", "10.0.0.1"})
	}

	withSource := pingCommand(config.Ping{
		From:  config.PingSource{Address: "10.255.0.1"},
		Count: 5,
		To:    "192.0.2.1",
	})
	if got, want := strings.Join(withSource, "\x00"), strings.Join([]string{"ping", "-c", "5", "-W", "1", "-I", "10.255.0.1", "192.0.2.1"}, "\x00"); got != want {
		t.Fatalf("pingCommand() with source = %#v, want %#v", withSource, []string{"ping", "-c", "5", "-W", "1", "-I", "10.255.0.1", "192.0.2.1"})
	}

	withoutNamespace := pingCommand(config.Ping{
		Count: 5,
		To:    "192.0.2.1",
	})
	if got, want := strings.Join(withoutNamespace, "\x00"), strings.Join([]string{"ping", "-c", "5", "-W", "1", "192.0.2.1"}, "\x00"); got != want {
		t.Fatalf("pingCommand() without namespace = %#v, want %#v", withoutNamespace, []string{"ping", "-c", "5", "-W", "1", "192.0.2.1"})
	}
}

func TestConfigureLinksUsesNetworkManagerForMainNamespace(t *testing.T) {
	t.Parallel()

	fakeNetwork := &fakeNetworkManager{}
	manager := &Manager{network: fakeNetwork}
	topology := &config.Topology{
		Lab: config.Lab{Name: "fabric"},
		Links: []config.Link{
			{
				Name: "lan-a",
				Type: "bridge",
				MTU:  9100,
				Members: []config.LinkMember{
					{Router: "r1", IfName: "eth1", IPv4: "192.0.2.1/24", MAC: "02:00:00:00:00:11"},
					{Router: "r2", IfName: "eth1", IPv4: "192.0.2.2/24", MAC: "02:00:00:00:00:12"},
				},
			},
			{
				Name: "core",
				Type: "p2p",
				MTU:  1500,
				Members: []config.LinkMember{
					{Router: "r1", IfName: "eth2", IPv4: "198.51.100.0/31"},
					{Router: "r2", IfName: "eth2", IPv4: "198.51.100.1/31"},
				},
			},
		},
	}
	snapshot := &state.LabState{
		OwnedBridges: make(map[string]string),
		Containers: map[string]state.ContainerState{
			"r1": {PID: 101},
			"r2": {PID: 102},
		},
	}

	if err := manager.configureLinks(topology, snapshot); err != nil {
		t.Fatalf("configureLinks() error = %v", err)
	}

	assertSequenceEqual(t, fakeNetwork.ops, [][]string{
		{"ensure-bridge", bridgeLinkName("fabric", "lan-a"), "9100"},
		{"create-bridge-attachment", bridgeLinkName("fabric", "lan-a"), bridgePortName("fabric", "lan-a", "r1", "eth1"), bridgePeerName("fabric", "lan-a", "r1", "eth1"), "101", "9100"},
		{"configure-attached-link", "101", bridgePeerName("fabric", "lan-a", "r1", "eth1"), "eth1", "192.0.2.1/24", "02:00:00:00:00:11", "9100"},
		{"create-bridge-attachment", bridgeLinkName("fabric", "lan-a"), bridgePortName("fabric", "lan-a", "r2", "eth1"), bridgePeerName("fabric", "lan-a", "r2", "eth1"), "102", "9100"},
		{"configure-attached-link", "102", bridgePeerName("fabric", "lan-a", "r2", "eth1"), "eth1", "192.0.2.2/24", "02:00:00:00:00:12", "9100"},
		{"create-p2p-attachment", p2pTempName("fabric", "core", "a"), "101", p2pTempName("fabric", "core", "b"), "102", "1500"},
		{"configure-attached-link", "101", p2pTempName("fabric", "core", "a"), "eth2", "198.51.100.0/31", "", "1500"},
		{"configure-attached-link", "102", p2pTempName("fabric", "core", "b"), "eth2", "198.51.100.1/31", "", "1500"},
	}, "network ops")
	if !reflect.DeepEqual(snapshot.OwnedBridges, map[string]string{"lan-a": bridgeLinkName("fabric", "lan-a")}) {
		t.Fatalf("bridge inventory = %v", snapshot.OwnedBridges)
	}
}

func TestConfigureLoopbacksUsesNetworkManager(t *testing.T) {
	t.Parallel()

	fakeNetwork := &fakeNetworkManager{}
	manager := &Manager{network: fakeNetwork}
	routers := map[string]config.ResolvedRouter{
		"r1": {Loopbacks: []string{"10.255.0.1/32", "2001:db8::1/128"}},
	}
	snapshot := &state.LabState{
		Containers: map[string]state.ContainerState{
			"r1": {PID: 101},
		},
	}

	if err := manager.configureLoopbacks(routers, snapshot); err != nil {
		t.Fatalf("configureLoopbacks() error = %v", err)
	}

	assertSequenceEqual(t, fakeNetwork.ops, [][]string{
		{"configure-loopback", "101", "10.255.0.1/32", "2001:db8::1/128"},
	}, "network ops")
}

func TestHasDaemonsRequiresEveryExpectedDaemon(t *testing.T) {
	t.Parallel()

	if !hasDaemons([]string{"zebra", "bgpd", "ospfd"}, []string{"zebra", "bgpd"}) {
		t.Fatalf("hasDaemons() = false, want true when all daemons are present")
	}
	if hasDaemons([]string{"zebra", "bgpd"}, []string{"zebra", "bgpd", "ospf6d"}) {
		t.Fatalf("hasDaemons() = true, want false when one daemon is missing")
	}
}

func TestWaitForVTYSHReturnsPromptlyOnContextCancellation(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		docker: &fakeDockerClient{
			execFn: func(ctx context.Context, _ string, _ []string) (docker.ExecResult, error) {
				return docker.ExecResult{}, ctx.Err()
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- manager.waitForVTYSH(ctx, "container-1", []string{"zebra"})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waitForVTYSH() error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waitForVTYSH() did not return promptly after context cancellation")
	}
}

func TestConfigureLinuxUsesNetworkManagerForMainNamespaceAndDockerExecForNamedNamespaces(t *testing.T) {
	t.Parallel()

	fakeDocker := &fakeDockerClient{}
	fakeNetwork := &fakeNetworkManager{}
	manager := &Manager{docker: fakeDocker, network: fakeNetwork}
	neighSuppress := true
	learning := false

	routers := map[string]config.ResolvedRouter{
		"r1": {
			Linux: config.Linux{
				VRFs: []config.VRF{
					{Name: "tenant", Table: 1100},
				},
				Bridges: []config.Bridge{
					{
						Name:        "br5000",
						Master:      "tenant",
						MAC:         "02:00:00:00:50:11",
						AddrGenMode: "none",
						Interfaces:  []string{"eth4"},
						VXLANS: []config.VXLAN{
							{
								Name:        "vxlan5000",
								VNI:         5000,
								Local:       "10.255.0.11",
								NoLearning:  true,
								AddrGenMode: "none",
								BridgeSlave: config.BridgeSlaveOptions{
									NeighSuppress: &neighSuppress,
									Learning:      &learning,
								},
							},
						},
						Namespaces: []config.Namespace{
							{
								Name:       "ns5000",
								IfName:     "eth0",
								MAC:        "02:00:00:00:50:21",
								Addresses:  []string{"10.50.0.21/24"},
								DefaultVia: "10.50.0.1",
							},
						},
					},
				},
				Bonds: []config.Bond{
					{
						Name:       "bond0",
						Mode:       "active-backup",
						Master:     "tenant",
						Interfaces: []string{"eth2"},
					},
				},
				Interfaces: []config.Interface{
					{
						Name:      "eth3",
						Master:    "tenant",
						Addresses: []string{"10.20.30.1/24"},
					},
				},
				Veths: []config.Veth{
					{
						Name:      "lan0",
						Peer:      "host0",
						Master:    "tenant",
						Addresses: []string{"10.10.20.1/24"},
						Namespace: &config.Namespace{
							Name:       "host",
							IfName:     "eth0",
							MAC:        "02:00:00:00:b0:11",
							Addresses:  []string{"10.10.20.11/24"},
							DefaultVia: "10.10.20.1",
						},
					},
				},
				Routes: []config.Route{
					{
						To:  "10.255.0.2/32",
						Via: "192.0.2.1",
						Dev: "eth1",
					},
				},
			},
		},
	}
	snapshot := &state.LabState{
		Containers: map[string]state.ContainerState{
			"r1": {ID: "container-1", PID: 101},
		},
	}

	if err := manager.configureLinux(context.Background(), routers, snapshot); err != nil {
		t.Fatalf("configureLinux() error = %v", err)
	}

	assertSequenceEqual(t, fakeNetwork.ops, [][]string{
		{"configure-vrf", "101", "tenant", "1100"},
		{"configure-bond", "101", "bond0", "active-backup", "tenant", "eth2", "", "", ""},
		{"configure-bridge", "101", "br5000", "tenant", "02:00:00:00:50:11", "none", "eth4", "vxlan5000|5000|10.255.0.11|0|true|none|true|false"},
		{"configure-bridge-namespace-veth", "101", "br5000", bridgeNamespaceHostVethName("br5000", "ns5000"), bridgeNamespacePeerVethName("br5000", "ns5000")},
		{"attach-link-master", "101", "bond0", "tenant"},
		{"configure-interface", "101", "eth3", "tenant", "", "", "10.20.30.1/24"},
		{"configure-veth", "101", "lan0", "host0", "tenant", "", "", "10.10.20.1/24", "true"},
		{"configure-route", "101", "10.255.0.2/32", "192.0.2.1", "eth1"},
	}, "network ops")

	assertSequenceEqual(t, fakeDocker.execs, [][]string{
		{"ip", "netns", "add", "ns5000"},
		{"ip", "link", "set", "dev", bridgeNamespacePeerVethName("br5000", "ns5000"), "netns", "ns5000"},
		{"ip", "netns", "exec", "ns5000", "ip", "link", "set", "dev", "lo", "up"},
		{"ip", "netns", "exec", "ns5000", "ip", "link", "set", "dev", bridgeNamespacePeerVethName("br5000", "ns5000"), "name", "eth0"},
		{"ip", "netns", "exec", "ns5000", "ip", "link", "set", "dev", "eth0", "address", "02:00:00:00:50:21"},
		{"ip", "netns", "exec", "ns5000", "ip", "addr", "replace", "10.50.0.21/24", "dev", "eth0"},
		{"ip", "netns", "exec", "ns5000", "ip", "link", "set", "dev", "eth0", "up"},
		{"ip", "netns", "exec", "ns5000", "ip", "route", "replace", "default", "via", "10.50.0.1"},
		{"ip", "netns", "add", "host"},
		{"ip", "link", "set", "dev", "host0", "netns", "host"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", "lo", "up"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", "host0", "name", "eth0"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", "eth0", "address", "02:00:00:00:b0:11"},
		{"ip", "netns", "exec", "host", "ip", "addr", "replace", "10.10.20.11/24", "dev", "eth0"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", "eth0", "up"},
		{"ip", "netns", "exec", "host", "ip", "route", "replace", "default", "via", "10.10.20.1"},
	}, "docker execs")
}

func TestRunRouterCommandsRespectsConfiguredOrder(t *testing.T) {
	t.Parallel()

	fakeDocker := &fakeDockerClient{daemons: []string{"zebra", "bgpd", "ospfd", "ospf6d", "isisd", "staticd", "pathd"}}
	manager := &Manager{docker: fakeDocker}
	markerPath := t.TempDir() + "/seeded"

	routers := map[string]config.ResolvedRouter{
		"r1": {
			Commands: []config.Command{
				{Kind: "shell", Run: "echo before"},
				{Kind: "vtysh", Run: "configure terminal\nhostname r1"},
				{Kind: "shell", Run: "echo after"},
			},
		},
	}
	snapshot := &state.LabState{
		Containers: map[string]state.ContainerState{
			"r1": {ID: "container-1"},
		},
	}
	prepared := map[string]frr.PrepareResult{
		"r1": {
			NeedsSeed:  true,
			MarkerPath: markerPath,
		},
	}

	if err := manager.runRouterCommands(context.Background(), routers, snapshot, prepared); err != nil {
		t.Fatalf("runRouterCommands() error = %v", err)
	}

	assertSequenceEqual(t, fakeDocker.execs, [][]string{
		{"sh", "-lc", "echo before"},
		{"sh", "-lc", renderVTYSH("configure terminal\nhostname r1")},
		{"sh", "-lc", "echo after"},
	}, "docker execs")
}

func summarizeVXLANS(vxlans []config.VXLAN) string {
	parts := make([]string, 0, len(vxlans))
	for _, vxlan := range vxlans {
		parts = append(parts, strings.Join([]string{
			vxlan.Name,
			strconv.Itoa(vxlan.VNI),
			vxlan.Local,
			strconv.Itoa(vxlan.DstPort),
			strconv.FormatBool(vxlan.NoLearning),
			vxlan.AddrGenMode,
			boolValue(vxlan.BridgeSlave.NeighSuppress),
			boolValue(vxlan.BridgeSlave.Learning),
		}, "|"))
	}
	return strings.Join(parts, ",")
}

func boolValue(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}

func assertSequenceEqual(t *testing.T, got, want [][]string, label string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s len = %d, want %d\ngot=%#v", label, len(got), len(want), got)
	}
	for i := range want {
		if strings.Join(got[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Fatalf("%s[%d] = %#v, want %#v", label, i, got[i], want[i])
		}
	}
}
