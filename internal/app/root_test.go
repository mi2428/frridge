package app

import (
	"bytes"
	"context"
	"testing"

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

func TestUpCommandForwardsFlags(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	cmd := NewRootCommand(service)
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
	cmd := NewRootCommand(service)
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
	cmd := NewRootCommand(service)
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
