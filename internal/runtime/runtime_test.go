package runtime

import (
	"context"
	"errors"
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
	daemons []string
	execs   [][]string
	execFn  func(context.Context, string, []string) (docker.ExecResult, error)
}

type fakeNetworkManager struct {
	ops [][]string
}

func (f *fakeDockerClient) CreateContainer(context.Context, docker.ContainerSpec) (string, error) {
	return "", nil
}

func (f *fakeDockerClient) StartContainer(context.Context, string) error {
	return nil
}

func (f *fakeDockerClient) InspectContainer(context.Context, string) (docker.Inspect, error) {
	return docker.Inspect{}, nil
}

func (f *fakeDockerClient) ListContainers(context.Context, map[string]string) ([]string, error) {
	return nil, nil
}

func (f *fakeDockerClient) RemoveContainer(context.Context, string) error {
	return nil
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
	return nil
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
