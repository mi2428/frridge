package multipass

import (
	"context"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"frridge/internal/config"
)

type fakeCLI struct {
	missing     bool
	info        Info
	arch        string
	launched    []Instance
	started     []string
	mounts      [][2]string
	transfers   [][2]string
	execs       []ExecSpec
	outputErr   error
	launchErr   error
	startErr    error
	mountErr    error
	transferErr error
	execErr     error
}

func (f *fakeCLI) Info(context.Context, string) (Info, error) {
	if f.missing {
		return Info{}, ErrInstanceNotFound
	}
	mounts := make(map[string]string, len(f.info.Mounts))
	maps.Copy(mounts, f.info.Mounts)
	return Info{State: f.info.State, Mounts: mounts}, nil
}

func (f *fakeCLI) Launch(_ context.Context, spec Instance) error {
	if f.launchErr != nil {
		return f.launchErr
	}
	f.missing = false
	f.launched = append(f.launched, spec)
	if f.info.Mounts == nil {
		f.info.Mounts = make(map[string]string)
	}
	if f.info.State == "" {
		f.info.State = "Stopped"
	}
	return nil
}

func (f *fakeCLI) Start(_ context.Context, instance string) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, instance)
	f.info.State = "Running"
	return nil
}

func (f *fakeCLI) Mount(_ context.Context, source, target string) error {
	if f.mountErr != nil {
		return f.mountErr
	}
	f.mounts = append(f.mounts, [2]string{source, target})
	if f.info.Mounts == nil {
		f.info.Mounts = make(map[string]string)
	}
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 {
		panic("unexpected mount target")
	}
	f.info.Mounts[parts[1]] = source
	return nil
}

func (f *fakeCLI) Transfer(_ context.Context, source, target string) error {
	if f.transferErr != nil {
		return f.transferErr
	}
	f.transfers = append(f.transfers, [2]string{source, target})
	return nil
}

func (f *fakeCLI) Exec(_ context.Context, _ string, spec ExecSpec) error {
	if f.execErr != nil {
		return f.execErr
	}
	f.execs = append(f.execs, spec)
	return nil
}

func (f *fakeCLI) Output(_ context.Context, _ string, spec ExecSpec) (string, error) {
	if f.outputErr != nil {
		return "", f.outputErr
	}
	if len(spec.Command) == 2 && spec.Command[0] == "uname" && spec.Command[1] == "-m" {
		return f.arch, nil
	}
	return "", nil
}

type fakeBuilder struct {
	result BuildResult
	repo   string
	arch   string
}

func (b *fakeBuilder) Build(_ context.Context, repoDir, goarch string) (BuildResult, error) {
	b.repo = repoDir
	b.arch = goarch
	return b.result, nil
}

func TestResolveRequestDefaultsAndSharedMount(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()

	resolved, err := resolveRequest(Request{
		RepoDir: repoDir,
		HostDir: repoDir,
	})
	if err != nil {
		t.Fatalf("resolveRequest() error = %v", err)
	}
	if got, want := resolved.instance.Name, DefaultInstanceName; got != want {
		t.Fatalf("instance.Name = %q, want %q", got, want)
	}
	if got, want := resolved.instance.Image, DefaultImage; got != want {
		t.Fatalf("instance.Image = %q, want %q", got, want)
	}
	if resolved.guestHostDir != resolved.guestRepoDir {
		t.Fatalf("guestHostDir = %q, want same as guestRepoDir %q", resolved.guestHostDir, resolved.guestRepoDir)
	}
	if !strings.HasPrefix(resolved.guestWorkDir, defaultGuestStateRoot+"/") {
		t.Fatalf("guestWorkDir = %q, want %q prefix", resolved.guestWorkDir, defaultGuestStateRoot+"/")
	}
}

func TestManagerEnsureLaunchesAndTransfersBinary(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	hostDir := t.TempDir()
	cli := &fakeCLI{
		missing: true,
		arch:    "aarch64\n",
	}
	builder := &fakeBuilder{
		result: BuildResult{
			ID:   "digest-arm64",
			Path: filepath.Join(t.TempDir(), "frridge"),
		},
	}
	manager := NewManager(cli, builder)

	env, err := manager.Ensure(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if len(cli.launched) != 1 {
		t.Fatalf("len(launched) = %d, want 1", len(cli.launched))
	}
	if len(cli.started) != 1 {
		t.Fatalf("len(started) = %d, want 1", len(cli.started))
	}
	if len(cli.mounts) != 2 {
		t.Fatalf("len(mounts) = %d, want 2", len(cli.mounts))
	}
	if builder.repo != repoDir {
		t.Fatalf("builder repo = %q, want %q", builder.repo, repoDir)
	}
	if builder.arch != "arm64" {
		t.Fatalf("builder arch = %q, want arm64", builder.arch)
	}
	if got, want := env.GuestBinary, path.Join(defaultGuestBinaryRoot, "digest-arm64", "frridge"); got != want {
		t.Fatalf("GuestBinary = %q, want %q", got, want)
	}
	if len(cli.transfers) != 1 {
		t.Fatalf("len(transfers) = %d, want 1", len(cli.transfers))
	}
	transferTarget := cli.transfers[0][1]
	wantPrefix := "mp-lab:" + env.GuestBinaryDir + "/.frridge-"
	if !strings.HasPrefix(transferTarget, wantPrefix) || !strings.HasSuffix(transferTarget, ".tmp") {
		t.Fatalf("transfer target = %q, want %q*.tmp", transferTarget, wantPrefix)
	}
	if got, want := cli.execs[len(cli.execs)-2].Command, []string{"chmod", "0755", strings.TrimPrefix(transferTarget, "mp-lab:")}; !slices.Equal(got, want) {
		t.Fatalf("chmod command = %#v, want %#v", got, want)
	}
	if got, want := cli.execs[len(cli.execs)-1].Command, []string{"mv", strings.TrimPrefix(transferTarget, "mp-lab:"), env.GuestBinary}; !slices.Equal(got, want) {
		t.Fatalf("mv command = %#v, want %#v", got, want)
	}
}

func TestManagerFrridgeWrapsGuestBinaryAndWorkDir(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	hostDir := t.TempDir()
	cli := &fakeCLI{
		info: Info{
			State:  "Running",
			Mounts: make(map[string]string),
		},
		arch: "x86_64\n",
	}
	builder := &fakeBuilder{
		result: BuildResult{
			ID:   "digest-amd64",
			Path: filepath.Join(t.TempDir(), "frridge"),
		},
	}
	manager := NewManager(cli, builder)

	if err := manager.Frridge(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	}, []string{"--file", "lab.yaml", "up"}); err != nil {
		t.Fatalf("Frridge() error = %v", err)
	}

	last := cli.execs[len(cli.execs)-1]
	wantPrefix := []string{
		"sudo",
		"env",
		config.WorkDirOverrideEnv + "=" + path.Join(defaultGuestStateRoot, shortHash(hostDir)),
		path.Join(defaultGuestBinaryRoot, "digest-amd64", "frridge"),
		"--file",
		"lab.yaml",
		"up",
	}
	if strings.Join(last.Command, "\x00") != strings.Join(wantPrefix, "\x00") {
		t.Fatalf("last command = %#v, want %#v", last.Command, wantPrefix)
	}
	if got, want := last.Dir, path.Join(defaultGuestMountRoot, shortHash(hostDir)); got != want {
		t.Fatalf("last.Dir = %q, want %q", got, want)
	}
}

func TestManagerEnsureBuildsCompanionImageWhenDockerfileExists(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(Dockerfile) error = %v", err)
	}

	hostDir := t.TempDir()
	cli := &fakeCLI{
		info: Info{
			State:  "Running",
			Mounts: make(map[string]string),
		},
		arch: "x86_64\n",
	}
	builder := &fakeBuilder{
		result: BuildResult{
			ID:   "digest-amd64",
			Path: filepath.Join(t.TempDir(), "frridge"),
		},
	}
	manager := NewManager(cli, builder)

	if _, err := manager.Ensure(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	}); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	found := false
	for _, spec := range cli.execs {
		if spec.Dir != path.Join(defaultGuestMountRoot, shortHash(repoDir)) {
			continue
		}
		if len(spec.Command) < 3 {
			continue
		}
		if spec.Command[0] == "bash" && spec.Command[1] == "-lc" && strings.Contains(spec.Command[2], "docker build -t \"$1\" .") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Ensure() did not run guest image bootstrap command: %#v", cli.execs)
	}
}

func TestMapGuestArch(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		want string
	}{
		{raw: "x86_64\n", want: "amd64"},
		{raw: "amd64", want: "amd64"},
		{raw: "aarch64", want: "arm64"},
		{raw: "arm64", want: "arm64"},
	} {
		got, err := mapGuestArch(test.raw)
		if err != nil {
			t.Fatalf("mapGuestArch(%q) error = %v", test.raw, err)
		}
		if got != test.want {
			t.Fatalf("mapGuestArch(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestBuildExecArgsWrapsDirAndEnv(t *testing.T) {
	t.Parallel()

	args := buildExecArgs("mp-lab", ExecSpec{
		Command: []string{"docker", "ps"},
		Dir:     "/work",
		Env: map[string]string{
			"B": "2",
			"A": "1",
		},
	})

	if got, want := args[:4], []string{"exec", "mp-lab", "--", "bash"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args prefix = %#v, want %#v", got, want)
	}
	if !slices.Equal(args[len(args)-6:], []string{"/work", "A=1", "B=2", "--", "docker", "ps"}) {
		t.Fatalf("args suffix = %#v, want wrapped command", args)
	}
}
