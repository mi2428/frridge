package main

import (
	"bytes"
	"context"
	"fmt"
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

func TestMigrateStateCommandForwardsOriginalTopology(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		service := &fakeService{}
		cmd := newRootCommand(service)
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		args := []string{"migrate-state"}
		if !missing {
			path := filepath.Join(t.TempDir(), "original.yaml")
			if err := os.WriteFile(path, []byte("apiVersion: frridge/v1alpha1\nlab:\n  name: test\nrouters:\n  r1: {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, "--file", path)
		}
		cmd.SetArgs(args)
		err := cmd.Execute()
		if missing {
			if err == nil || service.frridgeArgs != nil {
				t.Fatalf("missing original file: err=%v args=%v", err, service.frridgeArgs)
			}
		} else if err != nil || strings.Join(service.frridgeArgs, "\x00") != strings.Join([]string{"--file", "original.yaml", "migrate-state"}, "\x00") {
			t.Fatalf("migration forwarding: err=%v args=%v", err, service.frridgeArgs)
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

	wantHostDir, err := resolvedCoveragePath(filepath.Dir(topologyPath))
	if err != nil {
		t.Fatalf("resolvedCoveragePath() error = %v", err)
	}
	if got, want := service.frridgeReq.HostDir, wantHostDir; got != want {
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

func TestUpCommandRejectsSymlinkMountOutsideHostDir(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	hostDir := t.TempDir()
	outsideDir := t.TempDir()
	symlinkPath := filepath.Join(hostDir, "outside-link")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Skipf("Symlink() unsupported: %v", err)
	}

	topologyPath := filepath.Join(hostDir, "lab.yaml")
	if err := os.WriteFile(topologyPath, []byte(`
apiVersion: frridge/v1alpha1
lab:
  name: test
  defaults:
    image: frr
routers:
  r1:
    mounts:
      - source: ./outside-link
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
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "outside --host-dir") {
		t.Fatalf("Execute() error = %v, want symlink host-dir coverage failure", err)
	}
}

func TestTopologyCommandsValidateMissingMountCoverage(t *testing.T) {
	t.Parallel()

	hostDir := t.TempDir()
	insideDir := filepath.Join(hostDir, "inside")
	if err := os.Mkdir(insideDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	outsideDir := t.TempDir()
	for name, target := range map[string]string{
		"inside-link":            insideDir,
		"outside-link":           outsideDir,
		"dangling-link":          filepath.Join(outsideDir, "missing"),
		"dangling-internal-link": filepath.Join(insideDir, "missing"),
		"outside-via-parent":     filepath.Join(hostDir, "outside-link") + "/../missing",
		"inside-via-parent":      "inside/missing/../leaf",
		"missing-loop":           "missing/../missing-loop",
	} {
		if err := os.Symlink(target, filepath.Join(hostDir, name)); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}
	}

	for _, tc := range []struct {
		source    string
		wantError bool
	}{
		{source: "inside"},
		{source: "inside-link"},
		{source: "missing"},
		{source: "inside-link/missing/deep"},
		{source: "outside-link", wantError: true},
		{source: "outside-link/missing/deep", wantError: true},
		{source: "dangling-link", wantError: true},
		{source: "dangling-internal-link"},
		{source: "outside-via-parent", wantError: true},
		{source: "inside-via-parent"},
		{source: "missing-loop", wantError: true},
	} {
		for _, command := range []string{"up", "down", "ping", "console", "migrate-state"} {
			t.Run(command+"/"+tc.source, func(t *testing.T) {
				t.Parallel()
				service := &fakeService{}
				cmd := newRootCommand(service)
				cmd.SetOut(new(bytes.Buffer))
				cmd.SetErr(new(bytes.Buffer))
				topologyPath := filepath.Join(hostDir, command+"-"+strings.ReplaceAll(tc.source, "/", "-")+".yaml")
				data := fmt.Sprintf("apiVersion: frridge/v1alpha1\nlab:\n  name: test\nrouters:\n  r1:\n    mounts:\n      - source: %q\n        target: /lab\n", tc.source)
				if err := os.WriteFile(topologyPath, []byte(data), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				args := []string{command, "--file", topologyPath}
				if command == "console" {
					args = append(args, "r1")
				}
				cmd.SetArgs(args)
				err := cmd.Execute()
				if tc.wantError {
					if err == nil || !strings.Contains(err.Error(), "--host-dir") {
						t.Fatalf("Execute() error = %v, want host-dir coverage failure", err)
					}
					if service.frridgeArgs != nil {
						t.Fatal("Execute() called service before validating mount coverage")
					}
				} else if err != nil || service.frridgeArgs == nil {
					t.Fatalf("Execute() error = %v, args = %v, want guest command", err, service.frridgeArgs)
				}
			})
		}
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
