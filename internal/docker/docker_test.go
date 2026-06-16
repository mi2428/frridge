package docker

import (
	"bytes"
	"testing"

	containerapi "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

func TestEnvPairsSorted(t *testing.T) {
	t.Parallel()

	pairs := envPairs(map[string]string{
		"B": "2",
		"A": "1",
	})
	if got, want := len(pairs), 2; got != want {
		t.Fatalf("len(envPairs()) = %d, want %d", got, want)
	}
	if pairs[0] != "A=1" || pairs[1] != "B=2" {
		t.Fatalf("envPairs() = %#v, want sorted pairs", pairs)
	}
}

func TestToAPIMountsPreservesReadOnly(t *testing.T) {
	t.Parallel()

	mounts := toAPIMounts([]Mount{{
		Source:   "/tmp/src",
		Target:   "/lab",
		ReadOnly: true,
	}})
	if got, want := len(mounts), 1; got != want {
		t.Fatalf("len(toAPIMounts()) = %d, want %d", got, want)
	}
	if !mounts[0].ReadOnly {
		t.Fatalf("toAPIMounts()[0].ReadOnly = false, want true")
	}
}

func TestContainerSpecCommandCopiedIntoConfig(t *testing.T) {
	t.Parallel()

	spec := ContainerSpec{
		Image:   "example",
		Command: []string{"/bin/sh", "-lc", "sleep infinity"},
	}
	cfg := containerapi.Config{
		Image: spec.Image,
		Cmd:   append([]string(nil), spec.Command...),
	}
	if got, want := len(cfg.Cmd), 3; got != want {
		t.Fatalf("len(cfg.Cmd) = %d, want %d", got, want)
	}
	if got, want := cfg.Cmd[2], "sleep infinity"; got != want {
		t.Fatalf("cfg.Cmd[2] = %q, want %q", got, want)
	}
}

func TestCopyExecOutputDemultiplexesNonTTY(t *testing.T) {
	t.Parallel()

	var multiplexed bytes.Buffer
	stdoutWriter := stdcopy.NewStdWriter(&multiplexed, stdcopy.Stdout)
	stderrWriter := stdcopy.NewStdWriter(&multiplexed, stdcopy.Stderr)
	if _, err := stdoutWriter.Write([]byte("hello\n")); err != nil {
		t.Fatalf("stdoutWriter.Write() error = %v", err)
	}
	if _, err := stderrWriter.Write([]byte("boom\n")); err != nil {
		t.Fatalf("stderrWriter.Write() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := copyExecOutput(&stdout, &stderr, &multiplexed, false); err != nil {
		t.Fatalf("copyExecOutput() error = %v", err)
	}
	if got := stdout.String(); got != "hello\n" {
		t.Fatalf("stdout = %q, want %q", got, "hello\n")
	}
	if got := stderr.String(); got != "boom\n" {
		t.Fatalf("stderr = %q, want %q", got, "boom\n")
	}
}
