package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
	block  bool
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
		if s.block {
			fmt.Fprintln(os.Stdout, "CANCELED")
			select {}
		}
		return ctx.Err()
	case <-time.After(time.Second):
		return errors.New("CLI startup context did not receive signal")
	}
}

func TestNativeCLIRepeatedInterruptForcesShutdown(t *testing.T) {
	if os.Getenv("FRRIDGE_TEST_FORCE_INTERRUPT") == "1" {
		service := &interruptedService{signal: os.Interrupt, block: true}
		root := newRootCommand(service)
		root.SetArgs([]string{"--file", "lab.yaml", "up"})
		t.Fatalf("blocking startup unexpectedly returned: %v", executeCommand(root))
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestNativeCLIRepeatedInterruptForcesShutdown$")
	command.Env = append(os.Environ(), "FRRIDGE_TEST_FORCE_INTERRUPT=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	var waitErr error
	go func() { waitErr = command.Wait(); close(exited) }()
	defer func() { cancel(); <-exited }()
	canceled := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "CANCELED" {
				close(canceled)
				return
			}
		}
	}()
	select {
	case <-canceled:
	case <-exited:
		t.Fatalf("startup exited before graceful cancellation: %v", waitErr)
	case <-ctx.Done():
		t.Fatal("startup did not observe the first interrupt")
	}
	// Retry until termination is observed: AfterFunc and signal delivery are async.
	interrupts := time.NewTicker(10 * time.Millisecond)
	defer interrupts.Stop()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-exited:
			var exitErr *exec.ExitError
			if !errors.As(waitErr, &exitErr) {
				t.Fatalf("forced shutdown error = %v, want signal termination", waitErr)
			}
			status, ok := exitErr.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
				t.Fatalf("forced shutdown status = %v, want SIGINT", exitErr.Sys())
			}
			return
		case <-interrupts.C:
			if err := command.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatal("additional SIGINT remained suppressed during blocked shutdown")
		}
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
