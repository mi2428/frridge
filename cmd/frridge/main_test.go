package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	labruntime "frridge/internal/runtime"
)

type interruptedService struct {
	fakeService
	signal os.Signal
}

func (s *interruptedService) Up(ctx context.Context, _ string, _ labruntime.UpOptions) error {
	if ctx.Done() == nil {
		return errors.New("CLI startup has no cancellation context")
	}
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	if err := process.Signal(s.signal); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
		return errors.New("CLI startup context did not receive signal")
	}
}

func TestNativeCLISignalCancelsStartupContext(t *testing.T) {
	if signalName := os.Getenv("FRRIDGE_TEST_CLI_SIGNAL"); signalName != "" {
		interrupt := os.Interrupt
		if signalName == "SIGTERM" {
			interrupt = syscall.SIGTERM
		}
		service := &interruptedService{signal: interrupt}
		root := newRootCommand(service)
		root.SetArgs([]string{"--file", "lab.yaml", "up"})
		if err := executeCommand(root); !errors.Is(err, context.Canceled) {
			t.Fatalf("signal %s command error = %v, want context.Canceled", signalName, err)
		}
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, signalName := range []string{"SIGINT", "SIGTERM"} {
		t.Run(signalName, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestNativeCLISignalCancelsStartupContext$")
			command.Env = append(os.Environ(), "FRRIDGE_TEST_CLI_SIGNAL="+signalName)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("signal subprocess: %v\n%s", err, output)
			}
		})
	}
}

func TestNativeCLIStillRunsSuccessfulCommands(t *testing.T) {
	service := &fakeService{}
	root := newRootCommand(service)
	root.SetArgs([]string{"console", "r1", "--shell"})
	if err := executeCommand(root); err != nil {
		t.Fatal(err)
	}
	if service.consoleName != "r1" || !service.consoleOpts.Shell {
		t.Fatalf("console forwarding changed: %+v", service)
	}
}
