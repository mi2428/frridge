//go:build integration

package multipass

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"frridge/internal/config"
)

const (
	integrationOptInEnv = "FRRIDGE_RUN_INTEGRATION"
	keepSmokeEnv        = "FRRIDGE_KEEP_SMOKE"
	verifyLabEnv        = "FRRIDGE_VERIFY_LAB"
)

type smokeHarness struct {
	ctx             context.Context
	cli             *systemCLI
	env             Environment
	guestLabFile    string
	generatedLabDir string
	labName         string
}

func TestMultipassSmoke(t *testing.T) {
	if os.Getenv(integrationOptInEnv) != "1" {
		t.Skipf("set %s=1 to run Multipass integration tests", integrationOptInEnv)
	}
	if _, err := exec.LookPath("multipass"); err != nil {
		t.Skipf("multipass is not installed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	t.Cleanup(cancel)

	h := newSmokeHarness(t, ctx)
	h.cleanup(t)
	if os.Getenv(keepSmokeEnv) != "1" {
		t.Cleanup(func() {
			h.cleanup(t)
		})
	} else {
		t.Logf("keeping smoke lab because %s=1", keepSmokeEnv)
	}

	if !t.Run("up", h.testUp) {
		return
	}
	if !t.Run("console_persists", h.testConsolePersists) {
		return
	}
	t.Run("down_purge", h.testDownPurge)
}

func newSmokeHarness(t *testing.T, ctx context.Context) *smokeHarness {
	t.Helper()

	repoRoot := repoRoot(t)
	hostLabPath := resolveVerifyLab(t, repoRoot)
	topology, err := config.LoadFile(hostLabPath)
	if err != nil {
		t.Fatalf("load verify lab: %v", err)
	}

	builder, err := NewGoBuilder("")
	if err != nil {
		t.Fatalf("create go builder: %v", err)
	}
	cli := newSystemCLI()
	manager := NewManager(cli, builder)
	req := Request{
		RepoDir: repoRoot,
		HostDir: filepath.Dir(hostLabPath),
		Instance: Instance{
			Name:   envString("MP_NAME", DefaultInstanceName),
			Image:  envString("MP_IMAGE", DefaultImage),
			CPUs:   envInt(t, "MP_CPUS", DefaultCPUs),
			Memory: envString("MP_MEM", DefaultMemory),
			Disk:   envString("MP_DISK", DefaultDisk),
		},
	}

	env, err := manager.prepare(ctx, req)
	if err != nil {
		t.Fatalf("prepare multipass environment: %v", err)
	}

	return &smokeHarness{
		ctx:             ctx,
		cli:             cli,
		env:             env,
		guestLabFile:    filepath.Base(hostLabPath),
		generatedLabDir: path.Join(env.GuestWorkDir, "labs", topology.Lab.Name),
		labName:         topology.Lab.Name,
	}
}

func (h *smokeHarness) testUp(t *testing.T) {
	t.Helper()

	h.runFrridge(t, "up")

	names := h.guestOutput(t, "sudo", "docker", "ps", "--format", "{{.Names}}")
	requireLine(t, names, h.containerName("rt1"))
	requireLine(t, names, h.containerName("rt2"))

	rt1Addr := h.guestOutput(t, "sudo", "docker", "exec", h.containerName("rt1"), "ip", "-4", "addr", "show", "dev", "eth1")
	requireContains(t, rt1Addr, "192.0.2.0/31")

	rt2Addr := h.guestOutput(t, "sudo", "docker", "exec", h.containerName("rt2"), "ip", "-4", "addr", "show", "dev", "eth1")
	requireContains(t, rt2Addr, "192.0.2.1/31")

	ipForward := strings.TrimSpace(h.guestOutput(t, "sudo", "docker", "exec", h.containerName("rt1"), "cat", "/proc/sys/net/ipv4/ip_forward"))
	if ipForward != "1" {
		t.Fatalf("ip_forward = %q, want 1", ipForward)
	}

	rpFilter := strings.TrimSpace(h.guestOutput(t, "sudo", "docker", "exec", h.containerName("rt1"), "cat", "/proc/sys/net/ipv4/conf/all/rp_filter"))
	if rpFilter != "0" {
		t.Fatalf("rp_filter = %q, want 0", rpFilter)
	}

	running := h.guestOutput(t, "sudo", "docker", "exec", h.containerName("rt1"), "vtysh", "-c", "show running-config")
	requireLine(t, running, "hostname rt1")
	requireLine(t, running, "router bgp 65000")
}

func (h *smokeHarness) testConsolePersists(t *testing.T) {
	t.Helper()

	console := h.consoleOutput(t, "rt1", "show running-config\nexit\n")
	requireLine(t, console, "hostname rt1")

	h.consoleOutput(t, "rt1", strings.Join([]string{
		"configure terminal",
		"router bgp 65000",
		" neighbor 192.0.2.1 remote-as 65000",
		" neighbor 192.0.2.1 description persisted-via-console",
		"end",
		"write memory",
		"exit",
		"",
	}, "\n"))

	h.runFrridge(t, "down")
	h.runFrridge(t, "up")

	running := h.guestOutput(t, "sudo", "docker", "exec", h.containerName("rt1"), "vtysh", "-c", "show running-config")
	requireLine(t, running, " neighbor 192.0.2.1 remote-as 65000")
	requireLine(t, running, " neighbor 192.0.2.1 description persisted-via-console")
}

func (h *smokeHarness) testDownPurge(t *testing.T) {
	t.Helper()

	h.runFrridge(t, "down", "--purge")

	names := h.guestOutput(t, "sudo", "docker", "ps", "-a", "--format", "{{.Names}}")
	if strings.Contains(names, h.containerNamePrefix()) {
		t.Fatalf("docker ps -a still lists %q containers:\n%s", h.containerNamePrefix(), names)
	}

	bridges := h.guestOutput(t, "bash", "-lc", "ip link show type bridge || true")
	if strings.Contains(bridges, " frb") {
		t.Fatalf("bridge cleanup left frridge bridge behind:\n%s", bridges)
	}

	h.guestRun(t, "bash", "-lc", "test ! -e \"$1\"", "bash", h.generatedLabDir)
}

func (h *smokeHarness) cleanup(t *testing.T) {
	t.Helper()

	if err := h.tryFrridge("down", "--purge"); err != nil {
		t.Logf("cleanup down --purge: %v", err)
	}
}

func (h *smokeHarness) runFrridge(t *testing.T, args ...string) {
	t.Helper()

	if err := h.tryFrridge(args...); err != nil {
		t.Fatalf("run frridge %v: %v", args, err)
	}
}

func (h *smokeHarness) tryFrridge(args ...string) error {
	command := []string{
		"sudo",
		"env",
		config.WorkDirOverrideEnv + "=" + h.env.GuestWorkDir,
		h.env.GuestBinary,
		"--file",
		h.guestLabFile,
	}
	command = append(command, args...)
	return h.cli.Exec(h.ctx, h.env.InstanceName, ExecSpec{
		Command: command,
		Dir:     h.env.GuestHostDir,
	})
}

func (h *smokeHarness) guestOutput(t *testing.T, command ...string) string {
	t.Helper()

	output, err := h.cli.Output(h.ctx, h.env.InstanceName, ExecSpec{
		Command: command,
		Dir:     h.env.GuestHostDir,
	})
	if err != nil {
		t.Fatalf("guest output %v: %v", command, err)
	}
	return strings.ReplaceAll(output, "\r", "")
}

func (h *smokeHarness) guestRun(t *testing.T, command ...string) {
	t.Helper()

	if err := h.cli.Exec(h.ctx, h.env.InstanceName, ExecSpec{
		Command: command,
		Dir:     h.env.GuestHostDir,
	}); err != nil {
		t.Fatalf("guest command %v: %v", command, err)
	}
}

func (h *smokeHarness) consoleOutput(t *testing.T, router, input string) string {
	t.Helper()

	command := joinShellCommand([]string{
		"sudo",
		"env",
		config.WorkDirOverrideEnv + "=" + h.env.GuestWorkDir,
		h.env.GuestBinary,
		"--file",
		h.guestLabFile,
		"console",
		router,
	})

	output, err := h.cli.Output(h.ctx, h.env.InstanceName, ExecSpec{
		Command: []string{"script", "-qec", command, "/dev/null"},
		Dir:     h.env.GuestHostDir,
		Stdin:   strings.NewReader(input),
	})
	if err != nil {
		t.Fatalf("console %q: %v", router, err)
	}
	return strings.ReplaceAll(output, "\r", "")
}

func (h *smokeHarness) containerName(router string) string {
	return "frridge-" + h.labName + "-" + router
}

func (h *smokeHarness) containerNamePrefix() string {
	return "frridge-" + h.labName + "-"
}

func requireContains(t *testing.T, output, needle string) {
	t.Helper()

	if !strings.Contains(output, needle) {
		t.Fatalf("output does not contain %q:\n%s", needle, output)
	}
}

func requireLine(t *testing.T, output, want string) {
	t.Helper()

	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		if line == want {
			return
		}
	}
	t.Fatalf("output does not contain line %q:\n%s", want, output)
}

func resolveVerifyLab(t *testing.T, repoRoot string) string {
	t.Helper()

	labPath := envString(verifyLabEnv, filepath.Join("testdata", "smoke", "lab.yaml"))
	if !filepath.IsAbs(labPath) {
		labPath = filepath.Join(repoRoot, labPath)
	}
	absPath, err := filepath.Abs(labPath)
	if err != nil {
		t.Fatalf("resolve verify lab path: %v", err)
	}
	return absPath
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, value, err)
	}
	return parsed
}

func joinShellCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
