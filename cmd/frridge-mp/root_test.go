package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"frridge/internal/multipass"
)

type fakeService struct {
	ensureReq   multipass.Request
	ensureEnv   multipass.Environment
	shellReq    multipass.Request
	execReq     multipass.Request
	execCommand []string
	frridgeReq  multipass.Request
	frridgeArgs []string
}

func (f *fakeService) Ensure(_ context.Context, req multipass.Request) (multipass.Environment, error) {
	f.ensureReq = req
	return f.ensureEnv, nil
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

func TestEnsurePrintsPreparedPaths(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		ensureEnv: multipass.Environment{
			InstanceName: "mp-lab",
			GuestHostDir: "/guest/work",
			GuestBinary:  "/guest/bin/frridge",
			GuestWorkDir: "/guest/state",
		},
	}

	cmd := newRootCommand(service)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ensure"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"instance: mp-lab", "workspace: /guest/work", "binary: /guest/bin/frridge", "workdir: /guest/state"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want substring %q", output, want)
		}
	}
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
