package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"frridge/internal/buildinfo"
	"frridge/internal/multipass"
)

type fakeService struct {
	shellReq    multipass.Request
	execReq     multipass.Request
	execCommand []string
	frridgeReq  multipass.Request
	frridgeArgs []string
}

func (f *fakeService) Shell(_ context.Context, req multipass.Request) error {
	f.shellReq = req
	return nil
}

func (f *fakeService) Exec(_ context.Context, req multipass.Request, command []string) error {
	f.execReq = req
	f.execCommand = append([]string(nil), command...)
	return nil
}

func (f *fakeService) Frridge(_ context.Context, req multipass.Request, args []string) error {
	f.frridgeReq = req
	f.frridgeArgs = append([]string(nil), args...)
	return nil
}

func TestUpCommandRelativizesTopologyAgainstImplicitHostDir(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	topologyPath := filepath.Join(t.TempDir(), "lab.yaml")
	if err := os.WriteFile(topologyPath, []byte("apiVersion: frridge/v1alpha1\nlab:\n  name: test\n  defaults:\n    image: frr\nrouters:\n  r1: {}\nlinks:\n  - name: l1\n    type: p2p\n    members:\n      - { router: r1, ifname: eth1 }\n      - { router: r1, ifname: eth2 }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd.SetArgs([]string{"up", "--file", topologyPath, "--recreate"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := service.frridgeReq.HostDir, filepath.Dir(topologyPath); got != want {
		t.Fatalf("HostDir = %q, want %q", got, want)
	}
	if got, want := strings.Join(service.frridgeArgs, "\x00"), strings.Join([]string{"--file", "lab.yaml", "up", "--recreate"}, "\x00"); got != want {
		t.Fatalf("frridge args = %#v, want relative topology path", service.frridgeArgs)
	}
}

func TestConsoleCommandAllowsOptionalFile(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"console", "rt1", "--shell"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := strings.Join(service.frridgeArgs, "\x00"), strings.Join([]string{"console", "--shell", "rt1"}, "\x00"); got != want {
		t.Fatalf("frridge args = %#v, want %#v", service.frridgeArgs, []string{"console", "--shell", "rt1"})
	}
}

func TestUpCommandRejectsMountOutsideHostDir(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, "nested"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	topologyPath := filepath.Join(rootDir, "nested", "lab.yaml")
	if err := os.WriteFile(topologyPath, []byte(`
apiVersion: frridge/v1alpha1
lab:
  name: test
  defaults:
    image: frr
routers:
  r1:
    mounts:
      - source: ../outside
        target: /lab
links:
  - name: l1
    type: p2p
    members:
      - { router: r1, ifname: eth1 }
      - { router: r1, ifname: eth2 }
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd.SetArgs([]string{"up", "--file", topologyPath})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute() error = nil, want host-dir coverage failure")
	}
}

func TestRootCommandPrintsVersion(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := stdout.String(), "frridge-mp "+buildinfo.Version+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestPingCommandForwardsNamedCheck(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	topologyPath := filepath.Join(t.TempDir(), "lab.yaml")
	if err := os.WriteFile(topologyPath, []byte("apiVersion: frridge/v1alpha1\nlab:\n  name: test\n  defaults:\n    image: frr\nrouters:\n  r1: {}\n  r2: {}\nlinks:\n  - name: l1\n    type: p2p\n    members:\n      - { router: r1, ifname: eth1 }\n      - { router: r2, ifname: eth1 }\npings:\n  - name: reachability\n    from:\n      router: r1\n    to: 192.0.2.2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd.SetArgs([]string{"ping", "--file", topologyPath, "reachability"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := strings.Join(service.frridgeArgs, "\x00"), strings.Join([]string{"--file", "lab.yaml", "ping", "reachability"}, "\x00"); got != want {
		t.Fatalf("frridge args = %#v, want %#v", service.frridgeArgs, []string{"--file", "lab.yaml", "ping", "reachability"})
	}
}
