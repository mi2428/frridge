package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	containerapi "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/term"
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
	if err := copyExecOutput(context.Background(), &stdout, &stderr, &multiplexed, false); err != nil {
		t.Fatalf("copyExecOutput() error = %v", err)
	}
	if got := stdout.String(); got != "hello\n" {
		t.Fatalf("stdout = %q, want %q", got, "hello\n")
	}
	if got := stderr.String(); got != "boom\n" {
		t.Fatalf("stderr = %q, want %q", got, "boom\n")
	}
}

// execMock uses the real SDK's HTTP upgrade path, without a Docker daemon.
func execMock(t *testing.T, tty, stalled bool, exitCode int) (*sdkClient, <-chan struct{}) {
	t.Helper()
	attached := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/container-1/exec"):
			var options containerapi.ExecOptions
			if err := json.NewDecoder(r.Body).Decode(&options); err != nil || options.Tty != tty {
				t.Errorf("exec options = %+v, error = %v, want tty=%t", options, err, tty)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Id":"exec-1"}`)
		case strings.HasSuffix(r.URL.Path, "/exec/exec-1/start"):
			_, _ = io.Copy(io.Discard, r.Body)
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			if err := rw.Flush(); err != nil {
				t.Error(err)
				return
			}
			close(attached)
			if stalled {
				<-release
				return
			}
			if tty {
				_, _ = io.WriteString(conn, "hello\n")
			} else {
				_, _ = io.WriteString(stdcopy.NewStdWriter(conn, stdcopy.Stdout), "hello\n")
				_, _ = io.WriteString(stdcopy.NewStdWriter(conn, stdcopy.Stderr), "boom\n")
			}
		case strings.HasSuffix(r.URL.Path, "/exec/exec-1/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"ExitCode":%d}`, exitCode)
		case strings.HasSuffix(r.URL.Path, "/exec/exec-1/resize"):
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected Docker API request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(func() { close(release); server.Close() })
	api, err := client.NewClientWithOpts(client.WithHost(strings.Replace(server.URL, "http://", "tcp://", 1)), client.WithVersion("1.47"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = api.Close() })
	return &sdkClient{api: api}, attached
}

func checkExecCancellation(t *testing.T, interactive, tty bool) {
	t.Helper()
	c, attached := execMock(t, tty, true, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		if interactive {
			done <- c.ExecInteractive(ctx, "container-1", []string{"sh"})
		} else {
			_, err := c.Exec(ctx, "container-1", []string{"sh"})
			done <- err
		}
	}()
	select {
	case <-attached:
	case err := <-done:
		t.Fatalf("exec returned before attachment: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("exec did not attach")
	}
	select {
	case err := <-done:
		t.Fatalf("stalled stream returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("exec cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exec output copy stayed blocked after cancellation")
	}
}

func TestExecCancellationAfterAttach(t *testing.T) {
	stdin, stdout := os.Stdin, os.Stdout
	nonTTY, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = nonTTY, nonTTY
	defer func() { os.Stdin, os.Stdout = stdin, stdout; _ = nonTTY.Close() }()
	for _, interactive := range []bool{false, true} {
		t.Run("interactive="+strconv.FormatBool(interactive), func(t *testing.T) {
			checkExecCancellation(t, interactive, false)
		})
	}
}

type watchedContext struct {
	context.Context
	active     atomic.Int64
	registered atomic.Int64
}

// Hide the stdlib parent's private value so cancellation uses AfterFunc.
func (c *watchedContext) Value(any) any { return nil }

func (c *watchedContext) AfterFunc(f func()) func() bool {
	c.active.Add(1)
	c.registered.Add(1)
	stop := context.AfterFunc(c.Context, func() { c.active.Add(-1); f() })
	return func() bool {
		if stop() {
			c.active.Add(-1)
			return true
		}
		return false
	}
}

func TestExecNormalCompletionStopsWatcherAndPreservesOutput(t *testing.T) {
	t.Parallel()
	c, _ := execMock(t, false, false, 7)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &watchedContext{Context: parent}
	result, err := c.Exec(ctx, "container-1", []string{"sh"})
	if err != nil || result.Stdout != "hello\n" || result.Stderr != "boom\n" || result.ExitCode != 7 {
		t.Fatalf("exec result = %+v, error = %v", result, err)
	}
	if active := ctx.active.Load(); active != 0 {
		t.Fatalf("cancellation watchers after successful exec = %d, want 0", active)
	}
	if ctx.registered.Load() == 0 {
		t.Fatal("test context did not observe any cancellation watchers")
	}
}

func TestCopyExecOutputDoesNotNormalizeCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tty := range []bool{false, true} {
		if err := copyExecOutput(ctx, io.Discard, io.Discard, strings.NewReader(""), tty); !errors.Is(err, context.Canceled) {
			t.Fatalf("tty=%t empty output copy = %v, want context.Canceled", tty, err)
		}
	}
}

func TestExecInteractiveTTYRestoresTerminal(t *testing.T) {
	if os.Getenv("FRRIDGE_TEST_EXEC_TTY") != "1" {
		script, err := exec.LookPath("script")
		if err != nil {
			t.Skip("script(1) is required to provide a test PTY")
		}
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var command *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			command = exec.CommandContext(ctx, script, "-q", os.DevNull, binary, "-test.run=^TestExecInteractiveTTYRestoresTerminal$")
		case "linux":
			command = exec.CommandContext(ctx, script, "-q", "-e", "-c", "'"+strings.ReplaceAll(binary, "'", "'\\''")+"' -test.run='^TestExecInteractiveTTYRestoresTerminal$'", os.DevNull)
		default:
			t.Skip("PTY test supports Darwin and Linux")
		}
		command.Env = append(os.Environ(), "FRRIDGE_TEST_EXEC_TTY=1")
		input, keepOpen, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		defer keepOpen.Close()
		command.Stdin = input
		if output, err := command.CombinedOutput(); err != nil || !bytes.Contains(output, []byte("PASS")) {
			t.Fatalf("PTY subprocess: %v\n%s", err, output)
		}
		return
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		t.Fatal("script did not provide an interactive PTY")
	}
	for _, mode := range []string{"cancel", "success", "nonzero"} {
		t.Run(mode, func(t *testing.T) {
			before := readTerminalState(t)
			if mode == "cancel" {
				checkExecCancellation(t, true, true)
			} else {
				exitCode := 0
				if mode == "nonzero" {
					exitCode = 7
				}
				c, _ := execMock(t, true, false, exitCode)
				err := c.ExecInteractive(context.Background(), "container-1", []string{"sh"})
				if (exitCode == 0 && err != nil) || (exitCode != 0 && (err == nil || !strings.Contains(err.Error(), "exit code 7"))) {
					t.Fatalf("interactive exit=%d: %v", exitCode, err)
				}
			}
			after := readTerminalState(t)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("terminal not restored: before=%v, after=%v", before, after)
			}
		})
	}
}
