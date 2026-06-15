package main

import (
	"bytes"
	"context"
	"testing"

	"frridge/internal/buildinfo"
	labruntime "frridge/internal/runtime"
)

type fakeService struct {
	upPath      string
	upOptions   labruntime.UpOptions
	downPath    string
	downOptions labruntime.DownOptions
	consolePath string
	consoleName string
	consoleOpts labruntime.ConsoleOptions
	pingPath    string
	pingName    string
	pingResults []labruntime.PingResult
}

func (f *fakeService) Up(_ context.Context, topologyPath string, opts labruntime.UpOptions) error {
	f.upPath = topologyPath
	f.upOptions = opts
	return nil
}

func (f *fakeService) Down(_ context.Context, topologyPath string, opts labruntime.DownOptions) error {
	f.downPath = topologyPath
	f.downOptions = opts
	return nil
}

func (f *fakeService) Console(_ context.Context, topologyPath, router string, opts labruntime.ConsoleOptions) error {
	f.consolePath = topologyPath
	f.consoleName = router
	f.consoleOpts = opts
	return nil
}

func (f *fakeService) Ping(_ context.Context, topologyPath, name string) ([]labruntime.PingResult, error) {
	f.pingPath = topologyPath
	f.pingName = name
	return append([]labruntime.PingResult(nil), f.pingResults...), nil
}

func TestUpCommandForwardsFlags(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--file", "lab.yaml", "up", "--recreate", "--reseed"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if service.upPath != "lab.yaml" {
		t.Fatalf("upPath = %q, want lab.yaml", service.upPath)
	}
	if !service.upOptions.Recreate || !service.upOptions.Reseed {
		t.Fatalf("upOptions = %+v, want recreate+reseed", service.upOptions)
	}
}

func TestDownCommandRequiresFile(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := newRootCommand(service)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"down"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute() error = nil, want missing file error")
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
	if service.consoleName != "rt1" {
		t.Fatalf("consoleName = %q, want rt1", service.consoleName)
	}
	if !service.consoleOpts.Shell {
		t.Fatalf("consoleOpts = %+v, want shell=true", service.consoleOpts)
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

	if got, want := stdout.String(), "frridge "+buildinfo.Version+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestPingCommandRunsNamedCheckAndPrintsOutput(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		pingResults: []labruntime.PingResult{
			{
				Name:     "fabric",
				Router:   "rt1",
				Target:   "10.0.0.2",
				Output:   "PING 10.0.0.2\n",
				ExitCode: 0,
			},
		},
	}
	cmd := newRootCommand(service)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--file", "lab.yaml", "ping", "fabric"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := service.pingPath, "lab.yaml"; got != want {
		t.Fatalf("pingPath = %q, want %q", got, want)
	}
	if got, want := service.pingName, "fabric"; got != want {
		t.Fatalf("pingName = %q, want %q", got, want)
	}
	if got := stdout.String(); got != "PING 10.0.0.2\n" {
		t.Fatalf("stdout = %q, want raw ping output", got)
	}
}
