package runtime

import (
	"strings"
	"testing"

	"frridge/internal/config"
	"frridge/internal/docker"
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

	withoutNamespace := pingCommand(config.Ping{
		Count: 5,
		To:    "192.0.2.1",
	})
	if got, want := strings.Join(withoutNamespace, "\x00"), strings.Join([]string{"ping", "-c", "5", "-W", "1", "192.0.2.1"}, "\x00"); got != want {
		t.Fatalf("pingCommand() without namespace = %#v, want %#v", withoutNamespace, []string{"ping", "-c", "5", "-W", "1", "192.0.2.1"})
	}
}
