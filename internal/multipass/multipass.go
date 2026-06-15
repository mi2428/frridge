// Package multipass provides a thin wrapper that prepares a Linux VM and then
// delegates lab lifecycle commands to the normal frridge binary inside it.
package multipass

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"frridge/internal/config"
)

const (
	// DefaultInstanceName is the default Multipass instance used by frridge-mp.
	DefaultInstanceName = "frridge-dev"
	// DefaultImage is the default Ubuntu release launched for frridge-mp.
	DefaultImage = "24.04"
	// DefaultCPUs is the default CPU count used when launching the instance.
	DefaultCPUs = 2
	// DefaultMemory is the default guest memory size.
	DefaultMemory = "4G"
	// DefaultDisk is the default guest disk size.
	DefaultDisk = "20G"

	defaultGuestMountRoot  = "/home/ubuntu/.local/share/frridge-mp/mounts"
	defaultGuestStateRoot  = "/home/ubuntu/.local/share/frridge-mp/workdirs"
	defaultGuestBinaryRoot = "/home/ubuntu/.local/share/frridge-mp/bin"
)

// Service is the high-level API used by the frridge-mp CLI.
type Service interface {
	Ensure(ctx context.Context, req Request) (Environment, error)
	Shell(ctx context.Context, req Request) error
	Exec(ctx context.Context, req Request, command []string) error
	Frridge(ctx context.Context, req Request, args []string) error
}

// Request describes the host-side inputs required to project one workspace into
// a Multipass VM.
type Request struct {
	Instance Instance
	RepoDir  string
	HostDir  string
}

// Instance describes the guest VM that should host the Linux-native frridge
// execution.
type Instance struct {
	Name   string
	Image  string
	CPUs   int
	Memory string
	Disk   string
}

// Environment describes the guest-side paths that frridge-mp prepared for one
// request.
type Environment struct {
	InstanceName   string
	HostDir        string
	RepoDir        string
	GuestHostDir   string
	GuestRepoDir   string
	GuestWorkDir   string
	GuestBinaryDir string
	GuestBinary    string
}

// Info captures the parts of `multipass info --format json` that the manager
// needs for idempotent instance and mount handling.
type Info struct {
	State  string
	Mounts map[string]string
}

// ExecSpec describes one command executed inside the guest.
type ExecSpec struct {
	Command []string
	Dir     string
	Env     map[string]string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// BuildResult identifies a cached Linux build of the frridge binary.
type BuildResult struct {
	ID   string
	Path string
}

// Builder produces Linux frridge binaries on the host so the guest does not
// need a Go toolchain.
type Builder interface {
	Build(ctx context.Context, repoDir, goarch string) (BuildResult, error)
}

type multipassCLI interface {
	Info(ctx context.Context, instance string) (Info, error)
	Launch(ctx context.Context, spec Instance) error
	Start(ctx context.Context, instance string) error
	Mount(ctx context.Context, source, target string) error
	Transfer(ctx context.Context, source, target string) error
	Exec(ctx context.Context, instance string, spec ExecSpec) error
	Output(ctx context.Context, instance string, spec ExecSpec) (string, error)
}

type resolvedRequest struct {
	instance     Instance
	hostDir      string
	repoDir      string
	guestHostDir string
	guestRepoDir string
	guestWorkDir string
}

// Manager implements Service with the system Multipass CLI and a host-side Go
// builder.
type Manager struct {
	cli     multipassCLI
	builder Builder
}

// NewManager wires explicit Multipass and build dependencies together.
func NewManager(cli multipassCLI, builder Builder) *Manager {
	return &Manager{
		cli:     cli,
		builder: builder,
	}
}

// NewDefault constructs the production Multipass manager.
func NewDefault() (*Manager, error) {
	builder, err := NewGoBuilder("")
	if err != nil {
		return nil, err
	}
	return NewManager(newSystemCLI(), builder), nil
}

// Ensure makes sure the VM exists, the requested host directories are mounted,
// the guest dependencies are installed, and the Linux frridge binary is
// present.
func (m *Manager) Ensure(ctx context.Context, req Request) (Environment, error) {
	resolved, err := resolveRequest(req)
	if err != nil {
		return Environment{}, err
	}
	return m.ensure(ctx, resolved)
}

// Shell opens an interactive login shell rooted at the mounted host directory.
func (m *Manager) Shell(ctx context.Context, req Request) error {
	env, err := m.Ensure(ctx, req)
	if err != nil {
		return err
	}

	return m.cli.Exec(ctx, env.InstanceName, ExecSpec{
		Command: []string{"bash", "-l"},
		Dir:     env.GuestHostDir,
		Env: map[string]string{
			config.WorkDirOverrideEnv: env.GuestWorkDir,
		},
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

// Exec runs an arbitrary guest command after preparing the VM and mounts.
func (m *Manager) Exec(ctx context.Context, req Request, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("guest command must not be empty")
	}

	env, err := m.Ensure(ctx, req)
	if err != nil {
		return err
	}

	return m.cli.Exec(ctx, env.InstanceName, ExecSpec{
		Command: command,
		Dir:     env.GuestHostDir,
		Env: map[string]string{
			config.WorkDirOverrideEnv: env.GuestWorkDir,
		},
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

// Frridge delegates a CLI invocation to the guest-local frridge binary while
// keeping generated runtime state on the guest filesystem.
func (m *Manager) Frridge(ctx context.Context, req Request, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("frridge arguments must not be empty")
	}

	env, err := m.Ensure(ctx, req)
	if err != nil {
		return err
	}

	command := []string{
		"sudo",
		"env",
		config.WorkDirOverrideEnv + "=" + env.GuestWorkDir,
		env.GuestBinary,
	}
	command = append(command, args...)

	return m.cli.Exec(ctx, env.InstanceName, ExecSpec{
		Command: command,
		Dir:     env.GuestHostDir,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
}

func (m *Manager) ensure(ctx context.Context, req resolvedRequest) (Environment, error) {
	if err := m.ensureInstance(ctx, req.instance); err != nil {
		return Environment{}, err
	}

	build, err := m.guestBuild(ctx, req.instance.Name, req.repoDir)
	if err != nil {
		return Environment{}, err
	}

	env := Environment{
		InstanceName:   req.instance.Name,
		HostDir:        req.hostDir,
		RepoDir:        req.repoDir,
		GuestHostDir:   req.guestHostDir,
		GuestRepoDir:   req.guestRepoDir,
		GuestWorkDir:   req.guestWorkDir,
		GuestBinaryDir: path.Join(defaultGuestBinaryRoot, build.ID),
		GuestBinary:    path.Join(defaultGuestBinaryRoot, build.ID, "frridge"),
	}

	if err := m.ensureGuestDirs(ctx, env); err != nil {
		return Environment{}, err
	}

	info, err := m.cli.Info(ctx, req.instance.Name)
	if err != nil {
		return Environment{}, err
	}
	if err := ensureMount(ctx, m.cli, req.instance.Name, info, req.repoDir, env.GuestRepoDir); err != nil {
		return Environment{}, err
	}
	if env.GuestHostDir != env.GuestRepoDir || req.hostDir != req.repoDir {
		if err := ensureMount(ctx, m.cli, req.instance.Name, info, req.hostDir, env.GuestHostDir); err != nil {
			return Environment{}, err
		}
	}

	if err := m.bootstrapGuest(ctx, env); err != nil {
		return Environment{}, err
	}
	if err := m.cli.Transfer(ctx, build.Path, req.instance.Name+":"+env.GuestBinary); err != nil {
		return Environment{}, err
	}
	if err := m.cli.Exec(ctx, req.instance.Name, ExecSpec{
		Command: []string{"chmod", "0755", env.GuestBinary},
	}); err != nil {
		return Environment{}, err
	}

	return env, nil
}

func (m *Manager) ensureInstance(ctx context.Context, inst Instance) error {
	info, err := m.cli.Info(ctx, inst.Name)
	if err != nil {
		if err != ErrInstanceNotFound {
			return err
		}
		if err := m.cli.Launch(ctx, inst); err != nil {
			return err
		}
		info, err = m.cli.Info(ctx, inst.Name)
		if err != nil {
			return err
		}
	}

	if !strings.EqualFold(info.State, "running") {
		if err := m.cli.Start(ctx, inst.Name); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) guestBuild(ctx context.Context, instance, repoDir string) (BuildResult, error) {
	arch, err := m.cli.Output(ctx, instance, ExecSpec{
		Command: []string{"uname", "-m"},
	})
	if err != nil {
		return BuildResult{}, err
	}

	goarch, err := mapGuestArch(arch)
	if err != nil {
		return BuildResult{}, err
	}
	return m.builder.Build(ctx, repoDir, goarch)
}

func (m *Manager) ensureGuestDirs(ctx context.Context, env Environment) error {
	return m.cli.Exec(ctx, env.InstanceName, ExecSpec{
		Command: []string{
			"mkdir",
			"-p",
			env.GuestRepoDir,
			env.GuestHostDir,
			env.GuestWorkDir,
			env.GuestBinaryDir,
		},
	})
}

func (m *Manager) bootstrapGuest(ctx context.Context, env Environment) error {
	const script = `
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
if ! command -v docker >/dev/null 2>&1; then
	sudo apt-get update
	sudo apt-get install -y docker.io ca-certificates
fi
sudo systemctl enable --now docker
mkdir -p "$HOME/.local/share/frridge-mp"
`

	return m.cli.Exec(ctx, env.InstanceName, ExecSpec{
		Command: []string{"bash", "-lc", strings.TrimSpace(script)},
	})
}

func ensureMount(ctx context.Context, cli multipassCLI, instance string, info Info, source, target string) error {
	if mountedSource, ok := info.Mounts[target]; ok {
		if filepath.Clean(mountedSource) == filepath.Clean(source) {
			return nil
		}
		return fmt.Errorf("guest path %q is already mounted from %q", target, mountedSource)
	}
	return cli.Mount(ctx, source, instance+":"+target)
}

func resolveRequest(req Request) (resolvedRequest, error) {
	inst := req.Instance
	if strings.TrimSpace(inst.Name) == "" {
		inst.Name = DefaultInstanceName
	}
	if strings.TrimSpace(inst.Image) == "" {
		inst.Image = DefaultImage
	}
	if inst.CPUs == 0 {
		inst.CPUs = DefaultCPUs
	}
	if strings.TrimSpace(inst.Memory) == "" {
		inst.Memory = DefaultMemory
	}
	if strings.TrimSpace(inst.Disk) == "" {
		inst.Disk = DefaultDisk
	}
	if inst.CPUs < 1 {
		return resolvedRequest{}, fmt.Errorf("cpus must be positive")
	}

	hostDir, err := resolveDirectory(req.HostDir)
	if err != nil {
		return resolvedRequest{}, fmt.Errorf("resolve host dir: %w", err)
	}

	repoDir := strings.TrimSpace(req.RepoDir)
	if repoDir == "" {
		repoDir, err = detectRepoDir()
		if err != nil {
			return resolvedRequest{}, err
		}
	}
	repoDir, err = resolveDirectory(repoDir)
	if err != nil {
		return resolvedRequest{}, fmt.Errorf("resolve repo dir: %w", err)
	}

	guestRepoDir := path.Join(defaultGuestMountRoot, shortHash(repoDir))
	guestHostDir := path.Join(defaultGuestMountRoot, shortHash(hostDir))
	if hostDir == repoDir {
		guestHostDir = guestRepoDir
	}

	return resolvedRequest{
		instance:     inst,
		hostDir:      hostDir,
		repoDir:      repoDir,
		guestHostDir: guestHostDir,
		guestRepoDir: guestRepoDir,
		guestWorkDir: path.Join(defaultGuestStateRoot, shortHash(hostDir)),
	}, nil
}

func resolveDirectory(input string) (string, error) {
	dir := strings.TrimSpace(input)
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		dir = cwd
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", abs)
	}
	return abs, nil
}

func detectRepoDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "cmd", "frridge", "main.go")); err == nil {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not detect the frridge source tree; pass --repo-dir")
}

func mapGuestArch(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported guest architecture %q", strings.TrimSpace(raw))
	}
}

func shortHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])[:12]
}
