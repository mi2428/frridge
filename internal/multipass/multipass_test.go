package multipass

import (
	"context"
	"fmt"
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
	imageBuilds map[string]string
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
		return fmt.Errorf("unexpected mount target %q", target)
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
	if len(spec.Command) >= 6 && spec.Command[0] == "bash" && spec.Command[1] == "-lc" {
		if strings.Contains(spec.Command[2], "docker image inspect") {
			if buildID, ok := f.imageBuilds[spec.Command[4]]; ok {
				return buildID + "\n", nil
			}
			return "", nil
		}
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

type fakeImageBuilder struct {
	result ImageBuildResult
	repo   string
	arch   string
	err    error
}

func (b *fakeImageBuilder) Build(_ context.Context, repoDir, goarch string) (ImageBuildResult, error) {
	b.repo = repoDir
	b.arch = goarch
	if b.err != nil {
		return ImageBuildResult{}, b.err
	}
	return b.result, nil
}

func bashScript(spec ExecSpec) (string, bool) {
	switch {
	case len(spec.Command) >= 3 && spec.Command[0] == "bash" && spec.Command[1] == "-lc":
		return spec.Command[2], true
	case len(spec.Command) >= 4 && spec.Command[0] == "sudo" && spec.Command[1] == "bash" && spec.Command[2] == "-lc":
		return spec.Command[3], true
	default:
		return "", false
	}
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

func TestManagerPrepareLaunchesAndTransfersBinary(t *testing.T) {
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

	env, err := manager.prepare(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	})
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
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

func TestManagerPrepareImportsCompanionImageArchiveWhenHostCacheAvailable(t *testing.T) {
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
	imageBuilder := &fakeImageBuilder{
		result: ImageBuildResult{
			ID:   "image-amd64",
			Path: filepath.Join(t.TempDir(), "image.tar"),
			Tag:  "frridge-mp-cache:image-amd64",
		},
	}
	manager := NewManagerWithImageBuilder(cli, builder, imageBuilder)

	if _, err := manager.prepare(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	}); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	if imageBuilder.repo != repoDir {
		t.Fatalf("imageBuilder repo = %q, want %q", imageBuilder.repo, repoDir)
	}
	if imageBuilder.arch != "amd64" {
		t.Fatalf("imageBuilder arch = %q, want amd64", imageBuilder.arch)
	}
	if got, want := len(cli.transfers), 2; got != want {
		t.Fatalf("len(transfers) = %d, want %d", got, want)
	}

	foundArchiveTransfer := false
	wantArchiveTarget := "mp-lab:" + path.Join(defaultGuestBinaryRoot, "digest-amd64", ".image-amd64.tar")
	for _, transfer := range cli.transfers {
		if transfer[0] != imageBuilder.result.Path {
			continue
		}
		foundArchiveTransfer = true
		if got, want := transfer[1], wantArchiveTarget; got != want {
			t.Fatalf("image archive target = %q, want %q", got, want)
		}
	}
	if !foundArchiveTransfer {
		t.Fatalf("prepare() did not transfer cached image archive: %#v", cli.transfers)
	}

	foundImport := false
	for _, spec := range cli.execs {
		if slices.Equal(spec.Command, []string{"sudo", "docker", "load", "-i", path.Join(defaultGuestBinaryRoot, "digest-amd64", ".image-amd64.tar")}) {
			foundImport = true
		}
		if len(spec.Command) >= 4 && slices.Equal(spec.Command[:4], []string{"sudo", "docker", "buildx", "build"}) {
			t.Fatalf("prepare() unexpectedly ran guest image build: %#v", cli.execs)
		}
	}
	if !foundImport {
		t.Fatalf("prepare() did not import cached companion image archive: %#v", cli.execs)
	}
}

func TestManagerPrepareFallsBackToGuestImageBuildWhenHostCacheUnavailable(t *testing.T) {
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
	imageBuilder := &fakeImageBuilder{err: ErrHostDockerUnavailable}
	manager := NewManagerWithImageBuilder(cli, builder, imageBuilder)

	if _, err := manager.prepare(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	}); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	if got, want := len(cli.transfers), 1; got != want {
		t.Fatalf("len(transfers) = %d, want %d", got, want)
	}

	foundGuestBuild := false
	for _, spec := range cli.execs {
		if slices.Equal(spec.Command, []string{
			"sudo",
			"docker",
			"buildx",
			"build",
			"--load",
			"--label", guestImageBuildIDLabel + "=" + mustImageBuildID(t, repoDir, "amd64"),
			"-t", defaultGuestImage,
			".",
		}) {
			foundGuestBuild = true
			break
		}
	}
	if !foundGuestBuild {
		t.Fatalf("prepare() did not fall back to guest image build: %#v", cli.execs)
	}
}

func TestManagerPrepareSkipsCompanionImageRefreshWhenGuestImageMatchesBuildID(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(Dockerfile) error = %v", err)
	}
	buildID, err := imageBuildID(repoDir, "amd64")
	if err != nil {
		t.Fatalf("imageBuildID() error = %v", err)
	}

	hostDir := t.TempDir()
	cli := &fakeCLI{
		info: Info{
			State:  "Running",
			Mounts: make(map[string]string),
		},
		arch: "x86_64\n",
		imageBuilds: map[string]string{
			defaultGuestImage: buildID,
		},
	}
	builder := &fakeBuilder{
		result: BuildResult{
			ID:   "digest-amd64",
			Path: filepath.Join(t.TempDir(), "frridge"),
		},
	}
	imageBuilder := &fakeImageBuilder{}
	manager := NewManagerWithImageBuilder(cli, builder, imageBuilder)

	if _, err := manager.prepare(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	}); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	if imageBuilder.repo != "" || imageBuilder.arch != "" {
		t.Fatalf("imageBuilder unexpectedly ran: repo=%q arch=%q", imageBuilder.repo, imageBuilder.arch)
	}
	if got, want := len(cli.transfers), 1; got != want {
		t.Fatalf("len(transfers) = %d, want %d", got, want)
	}
	for _, spec := range cli.execs {
		if len(spec.Command) >= 4 && slices.Equal(spec.Command[:4], []string{"sudo", "docker", "buildx", "build"}) {
			t.Fatalf("prepare() unexpectedly refreshed companion image: %#v", cli.execs)
		}
		if len(spec.Command) >= 4 && slices.Equal(spec.Command[:4], []string{"sudo", "docker", "load", "-i"}) {
			t.Fatalf("prepare() unexpectedly refreshed companion image: %#v", cli.execs)
		}
	}
}

func mustImageBuildID(t *testing.T, repoDir, goarch string) string {
	t.Helper()

	id, err := imageBuildID(repoDir, goarch)
	if err != nil {
		t.Fatalf("imageBuildID() error = %v", err)
	}
	return id
}

func TestManagerPrepareBootstrapsMPLSKernelSupport(t *testing.T) {
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

	if _, err := manager.prepare(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	}); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	for _, spec := range cli.execs {
		script, ok := bashScript(spec)
		if !ok {
			continue
		}
		if strings.Contains(script, "linux-modules-extra-$(uname -r)") &&
			strings.Contains(script, "/etc/modules-load.d/frridge-mp.conf") &&
			strings.Contains(script, "modprobe") &&
			strings.Contains(script, "mpls_router") &&
			strings.Contains(script, "mpls_iptunnel") &&
			strings.Contains(script, "vrf") {
			return
		}
	}

	t.Fatalf("prepare() did not run MPLS/VRF bootstrap command: %#v", cli.execs)
}

func TestManagerPrepareBootstrapsMakeForGuestWorkflows(t *testing.T) {
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

	if _, err := manager.prepare(context.Background(), Request{
		RepoDir: repoDir,
		HostDir: hostDir,
		Instance: Instance{
			Name: "mp-lab",
		},
	}); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	for _, spec := range cli.execs {
		script, ok := bashScript(spec)
		if !ok {
			continue
		}
		if strings.Contains(script, "command -v make") {
			return
		}
	}

	t.Fatalf("prepare() did not ensure guest make availability: %#v", cli.execs)
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

func TestBuildShellScriptWrapsDirAndEnv(t *testing.T) {
	t.Parallel()

	markers := &shellMarkers{output: "output-token", status: "status-token="}
	script := buildShellScript(ExecSpec{
		Command: []string{"docker", "ps"},
		Dir:     "/work",
		Env: map[string]string{
			"B": "2",
			"A": "1",
		},
	}, markers)

	for _, want := range []string{
		"set -euo pipefail\n",
		"cd '/work'\n",
		"export A='1'\n",
		"export B='2'\n",
		"printf 'output-token\\n'\n",
		"set +e\n'docker' 'ps'\nstatus=$?\nprintf 'status-token=%s\\n' \"$status\"\nexit 0\n",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("buildShellScript() missing %q in %q", want, script)
		}
	}
}

func TestStripOutputBannerReturnsOnlyCommandOutput(t *testing.T) {
	t.Parallel()

	markers := shellMarkers{output: "output-token", status: "status-token="}
	output := "banner\n" + markers.output + "\naarch64\n"
	if got, want := stripOutputBanner(output, markers), "aarch64\n"; got != want {
		t.Fatalf("stripOutputBanner() = %q, want %q", got, want)
	}
}

func TestParseShellOutputReturnsExitStatus(t *testing.T) {
	t.Parallel()

	markers := shellMarkers{output: "output-token", status: "status-token="}
	output, exitCode, err := parseShellOutput("banner\n"+markers.output+"\naarch64\n"+markers.status+"17\n", markers)
	if err != nil {
		t.Fatalf("parseShellOutput() error = %v", err)
	}
	if output != "aarch64\n" {
		t.Fatalf("parseShellOutput() output = %q, want %q", output, "aarch64\n")
	}
	if exitCode != 17 {
		t.Fatalf("parseShellOutput() exitCode = %d, want 17", exitCode)
	}
}

func TestParseShellOutputKeepsCommandOutputThatLooksLikeStatusLine(t *testing.T) {
	t.Parallel()

	markers := shellMarkers{output: "output-token", status: "status-token="}
	raw := "banner\n" + markers.output + "\nstatus-token-other=17\n" + markers.status + "0\n"
	output, exitCode, err := parseShellOutput(raw, markers)
	if err != nil {
		t.Fatalf("parseShellOutput() error = %v", err)
	}
	if output != "status-token-other=17\n" {
		t.Fatalf("parseShellOutput() output = %q, want %q", output, "status-token-other=17\n")
	}
	if exitCode != 0 {
		t.Fatalf("parseShellOutput() exitCode = %d, want 0", exitCode)
	}
}

func TestFirstFrridgeCommandSkipsGlobalFlags(t *testing.T) {
	t.Parallel()

	args := []string{"--file", "/guest/lab.yaml", "console", "--shell", "r1"}
	if got, want := firstFrridgeCommand(args), "console"; got != want {
		t.Fatalf("firstFrridgeCommand() = %q, want %q", got, want)
	}
}
