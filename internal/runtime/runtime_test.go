package runtime

import (
	"context"
	"strings"
	"testing"

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

func (f *fakeDockerClient) Exec(_ context.Context, _ string, cmd []string) (docker.ExecResult, error) {
	if len(cmd) == 3 && cmd[0] == "vtysh" && cmd[1] == "-c" && cmd[2] == "show daemons" {
		return docker.ExecResult{ExitCode: 0, Stdout: strings.Join(f.daemons, " ")}, nil
	}
	f.execs = append(f.execs, append([]string(nil), cmd...))
	return docker.ExecResult{ExitCode: 0}, nil
}

func (f *fakeDockerClient) ExecInteractive(context.Context, string, []string) error {
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

func TestRouteCommandIncludesViaAndDevice(t *testing.T) {
	t.Parallel()

	command := routeCommand(config.Route{
		To:  "10.255.0.2/32",
		Via: "192.0.2.1",
		Dev: "eth1",
	})
	if got, want := strings.Join(command, "\x00"), strings.Join([]string{"ip", "route", "replace", "10.255.0.2/32", "via", "192.0.2.1", "dev", "eth1"}, "\x00"); got != want {
		t.Fatalf("routeCommand() = %#v, want %#v", command, []string{"ip", "route", "replace", "10.255.0.2/32", "via", "192.0.2.1", "dev", "eth1"})
	}
}

func TestVXLANCommandUsesEVPNFriendlyDefaults(t *testing.T) {
	t.Parallel()

	command := vxlanCommand(config.VXLAN{
		Name:       "vxlan100",
		VNI:        100,
		Local:      "10.255.0.11",
		NoLearning: true,
	})
	if got, want := strings.Join(command, "\x00"), strings.Join([]string{"ip", "link", "add", "name", "vxlan100", "type", "vxlan", "id", "100", "local", "10.255.0.11", "dstport", "4789", "nolearning"}, "\x00"); got != want {
		t.Fatalf("vxlanCommand() = %#v, want %#v", command, []string{"ip", "link", "add", "name", "vxlan100", "type", "vxlan", "id", "100", "local", "10.255.0.11", "dstport", "4789", "nolearning"})
	}
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

func TestConfigureLinuxBridgesNamespacesAndRoutes(t *testing.T) {
	t.Parallel()

	fakeDocker := &fakeDockerClient{}
	manager := &Manager{docker: fakeDocker}
	routers := map[string]config.ResolvedRouter{
		"r1": {
			Linux: config.Linux{
				Bridges: []config.Bridge{
					{
						Name:       "br10",
						Addresses:  []string{"10.10.20.1/24"},
						Interfaces: []string{"eth2"},
						VXLANS: []config.VXLAN{
							{
								Name:       "vxlan100",
								VNI:        100,
								Local:      "10.255.0.11",
								NoLearning: true,
							},
						},
						Namespaces: []config.Namespace{
							{
								Name:       "host",
								IfName:     "eth0",
								MAC:        "02:00:00:00:b0:11",
								Addresses:  []string{"10.10.20.11/24"},
								DefaultVia: "10.10.20.1",
							},
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
			"r1": {ID: "container-1"},
		},
	}

	if err := manager.configureLinux(context.Background(), routers, snapshot); err != nil {
		t.Fatalf("configureLinux() error = %v", err)
	}

	hostVeth := bridgeNamespaceHostVethName("br10", "host")
	peerVeth := bridgeNamespacePeerVethName("br10", "host")
	want := [][]string{
		{"ip", "link", "add", "name", "br10", "type", "bridge"},
		{"ip", "addr", "replace", "10.10.20.1/24", "dev", "br10"},
		{"ip", "link", "set", "dev", "br10", "up"},
		{"ip", "link", "set", "dev", "eth2", "master", "br10"},
		{"ip", "link", "set", "dev", "eth2", "up"},
		{"ip", "link", "add", "name", "vxlan100", "type", "vxlan", "id", "100", "local", "10.255.0.11", "dstport", "4789", "nolearning"},
		{"ip", "link", "set", "dev", "vxlan100", "master", "br10"},
		{"ip", "link", "set", "dev", "vxlan100", "up"},
		{"ip", "netns", "add", "host"},
		{"ip", "link", "add", "name", hostVeth, "type", "veth", "peer", "name", peerVeth},
		{"ip", "link", "set", "dev", hostVeth, "master", "br10"},
		{"ip", "link", "set", "dev", hostVeth, "up"},
		{"ip", "link", "set", "dev", peerVeth, "netns", "host"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", "lo", "up"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", peerVeth, "name", "eth0"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", "eth0", "address", "02:00:00:00:b0:11"},
		{"ip", "netns", "exec", "host", "ip", "addr", "replace", "10.10.20.11/24", "dev", "eth0"},
		{"ip", "netns", "exec", "host", "ip", "link", "set", "dev", "eth0", "up"},
		{"ip", "netns", "exec", "host", "ip", "route", "replace", "default", "via", "10.10.20.1"},
		{"ip", "route", "replace", "10.255.0.2/32", "via", "192.0.2.1", "dev", "eth1"},
	}

	if len(fakeDocker.execs) != len(want) {
		t.Fatalf("len(execs) = %d, want %d\nexecs=%#v", len(fakeDocker.execs), len(want), fakeDocker.execs)
	}
	for i := range want {
		if got, wantJoined := strings.Join(fakeDocker.execs[i], "\x00"), strings.Join(want[i], "\x00"); got != wantJoined {
			t.Fatalf("exec[%d] = %#v, want %#v", i, fakeDocker.execs[i], want[i])
		}
	}
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

	want := [][]string{
		{"sh", "-lc", "echo before"},
		{"sh", "-lc", renderVTYSH("configure terminal\nhostname r1")},
		{"sh", "-lc", "echo after"},
	}
	if len(fakeDocker.execs) != len(want) {
		t.Fatalf("len(execs) = %d, want %d\nexecs=%#v", len(fakeDocker.execs), len(want), fakeDocker.execs)
	}
	for i := range want {
		if got, wantJoined := strings.Join(fakeDocker.execs[i], "\x00"), strings.Join(want[i], "\x00"); got != wantJoined {
			t.Fatalf("exec[%d] = %#v, want %#v", i, fakeDocker.execs[i], want[i])
		}
	}
}
