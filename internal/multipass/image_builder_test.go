package multipass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/template"
)

const testImageID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func imageTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func fakeHostDocker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const script = `#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_DOCKER_LOG"
case "$1 ${2:-}" in
  'info ') exit 0 ;;
  'version --format'|'buildx version') exit 0 ;;
  'buildx build')
    if [ "${FAKE_DOCKER_FAIL:-}" = build ]; then exit 17; fi
    inputs=''
    while (($#)); do
      case "$1" in
        --iidfile) iid=$2; shift ;;
        --label) inputs=${2#io.frridge.build-inputs=}; shift ;;
      esac
      shift
    done
    printf 'sha256:%s\n' "$FAKE_IMAGE_ID" > "$iid"
    printf '%s' "$inputs" > "$FAKE_DOCKER_INPUTS"
    ;;
  'image inspect')
    printf '{"ID":"sha256:%s","Inputs":"%s"}\n' "${FAKE_INSPECT_ID:-$FAKE_IMAGE_ID}" "$(cat "$FAKE_DOCKER_INPUTS")"
    ;;
  'image ls')
    if [ "${FAKE_DOCKER_FAIL:-}" = lookup ]; then exit 21; fi
    ;;
  'image tag') ;;
  'save -o')
    printf 'archive-%s' "$FAKE_IMAGE_ID" > "$3"
    if [ "${FAKE_DOCKER_FAIL:-}" = save ]; then exit 18; fi
    ;;
  *) exit 19 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DOCKER_LOG", log)
	t.Setenv("FAKE_DOCKER_INPUTS", filepath.Join(dir, "inputs"))
	t.Setenv("FAKE_IMAGE_ID", testImageID)
	t.Setenv("FAKE_INSPECT_ID", "")
	t.Setenv("FAKE_DOCKER_FAIL", "")
	t.Setenv("FRR_GIT_REF", "")
	t.Setenv("LIBYANG_GIT_REF", "")
	return log
}

func TestDockerImageFreshCachedOfflineAndRefresh(t *testing.T) {
	log := fakeHostDocker(t)
	repo := imageTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "Dockerfile"), []byte("ARG FRR_GIT_REF=frr-custom-pin\nARG LIBYANG_GIT_REF=libyang-custom-pin\nFROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := NewDockerImageBuilder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result, err := b.Cached(repo, "amd64"); err != nil || result.ID != "" {
		t.Fatalf("cold cache = %#v, %v", result, err)
	}
	first, err := b.Build(t.Context(), repo, "amd64")
	if err != nil || first.ID != testImageID || first.Legacy {
		t.Fatalf("fresh build = %#v, %v", first, err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--no-cache-filter source", "--platform linux/amd64", "--iidfile"} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("Docker calls missing %q", want)
		}
	}
	if strings.Contains(string(calls), "--build-arg") {
		t.Fatal("host build replaced custom Dockerfile defaults without an override")
	}
	t.Setenv("PATH", t.TempDir()) // Neither Docker nor git is available offline.
	if cached, err := b.Cached(repo, "amd64"); err != nil || cached != first {
		t.Fatalf("offline cached build = %#v, %v", cached, err)
	}
	if after, _ := os.ReadFile(log); string(after) != string(calls) {
		t.Fatal("cached lookup contacted Docker")
	}
	t.Setenv("PATH", filepath.Dir(log)+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("FAKE_IMAGE_ID", strings.Repeat("b", 64))
	second, err := b.Build(t.Context(), repo, "amd64")
	if err != nil || second.ID == first.ID || second.Path == first.Path {
		t.Fatalf("refreshed build = %#v, %v", second, err)
	}
	if cached, err := b.Cached(repo, "amd64"); err != nil || cached != second {
		t.Fatalf("refreshed lookup = %#v, %v", cached, err)
	}
	if err := checkImageArchive(first.Path); err != nil {
		t.Fatalf("previous archive lost: %v", err)
	}
}

func TestImageRefsUseOnlyExplicitOverridesInHostGuestAndMake(t *testing.T) {
	for _, tc := range []struct{ frr, libyang string }{
		{},
		{frr: "refs/pull/123/head"},
		{libyang: "libyang-custom-pin"},
		{frr: "frr-custom-pin", libyang: strings.Repeat("c", 40)},
	} {
		t.Run(tc.frr+"/"+tc.libyang, func(t *testing.T) {
			t.Setenv("FRR_GIT_REF", tc.frr)
			t.Setenv("LIBYANG_GIT_REF", tc.libyang)
			args := imageBuildArgs("inputs", "image.id") // Shared by host and guest.
			output, err := exec.CommandContext(t.Context(), "make", "--no-print-directory", "-C", "../..", "-n", "image", "FRR_GIT_REF="+tc.frr, "LIBYANG_GIT_REF="+tc.libyang).CombinedOutput()
			if err != nil {
				t.Fatalf("make image dry run: %v, %s", err, output)
			}
			for name, value := range map[string]string{"FRR_GIT_REF": tc.frr, "LIBYANG_GIT_REF": tc.libyang} {
				if slices.Contains(args, name+"="+value) != (value != "") || strings.Contains(string(output), "--build-arg "+name+"=") != (value != "") {
					t.Fatalf("%s override=%q, args=%v, make=%s", name, value, args, output)
				}
			}
		})
	}
}

func TestMakeImagePublishesLatestOnlyAfterSuccessfulBuild(t *testing.T) {
	for _, failure := range []string{"", "build"} {
		t.Run("failure="+failure, func(t *testing.T) {
			log := fakeHostDocker(t)
			t.Setenv("FAKE_DOCKER_FAIL", failure)
			cmd := exec.CommandContext(t.Context(), "make", "--no-print-directory", "-C", "../..", "image", "DOCKER="+filepath.Join(filepath.Dir(log), "docker"))
			cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
			output, err := cmd.CombinedOutput()
			if (err != nil) != (failure != "") {
				t.Fatalf("make image error=%v, failure=%q, output=%s", err, failure, output)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "image tag sha256:"+testImageID+" "+defaultGuestImage) != (failure == "") || strings.Contains(string(calls), "-t "+defaultGuestImage) {
				t.Fatal("make image did not stage publication of latest")
			}
		})
	}
}

func TestDockerImageRefreshFailureRetainsArchiveAndMetadata(t *testing.T) {
	fakeHostDocker(t)
	repo := imageTestRepo(t)
	b, err := NewDockerImageBuilder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := b.Build(t.Context(), repo, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"build", "save", "identity"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv("FAKE_IMAGE_ID", strings.Repeat("b", 64))
			t.Setenv("FAKE_DOCKER_FAIL", failure)
			if failure == "identity" {
				t.Setenv("FAKE_INSPECT_ID", strings.Repeat("c", 64))
			}
			if _, err := b.Build(t.Context(), repo, "amd64"); err == nil {
				t.Fatal("failed refresh returned success")
			}
			if cached, err := b.Cached(repo, "amd64"); err != nil || cached != first {
				t.Fatalf("previous lookup changed: %#v, %v", cached, err)
			}
			if data, _ := os.ReadFile(first.Path); string(data) != string(original) {
				t.Fatal("previous archive changed")
			}
		})
	}
}

func TestDockerImageCustomRefsAndLegacyPolicy(t *testing.T) {
	log := fakeHostDocker(t)
	repo := imageTestRepo(t)
	b, err := NewDockerImageBuilder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy := imageArchive(filepath.Join(b.cacheDir, "images"), mustImageBuildID(t, repo, "amd64"))
	if err := os.MkdirAll(filepath.Dir(legacy.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy.Path, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Cached(repo, "amd64"); err != nil || got.ID != legacy.ID || !got.Legacy {
		t.Fatalf("legacy lookup = %#v, %v", got, err)
	}
	defaultKey, err := imageInputsID(repo, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FRR_GIT_REF", "refs/pull/123/head")
	t.Setenv("LIBYANG_GIT_REF", strings.Repeat("c", 40))
	customKey, err := imageInputsID(repo, "amd64")
	if err != nil || customKey == defaultKey {
		t.Fatalf("custom inputs ID = %q, %v", customKey, err)
	}
	if got, err := b.Cached(repo, "amd64"); err != nil || got.ID != "" {
		t.Fatalf("legacy accepted for custom refs: %#v, %v", got, err)
	}
	if _, err := b.Build(t.Context(), repo, "arm64"); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FRR_GIT_REF=refs/pull/123/head", "LIBYANG_GIT_REF=" + strings.Repeat("c", 40), "--platform linux/arm64"} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("custom build missing %q", want)
		}
	}
}

func TestImageIdentityRejectsInvalidAndInconsistentMetadata(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "sha256:../bad", "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("g", 64)} {
		if _, err := parseDockerImageID(id); err == nil {
			t.Fatalf("accepted invalid ID %q", id)
		}
	}
	repo := imageTestRepo(t)
	b, err := NewDockerImageBuilder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := imageInputsID(repo, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.cacheDir, "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, metadata := range []string{"invalid", "sha256:" + testImageID} {
		if err := os.WriteFile(filepath.Join(dir, inputs+".id"), []byte(metadata), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Cached(repo, "amd64"); err == nil {
			t.Fatal("invalid metadata or missing archive silently accepted")
		}
	}
}

func TestDockerfileMaterializesAndRecordsSourceCommits(t *testing.T) {
	// Execute the actual source-stage RUN with mock git, not a second copy of it.
	dir := t.TempDir()
	data, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "COPY --from=source /src/source-revisions /usr/share/frridge/source-revisions") {
		t.Fatal("runtime image does not retain source provenance")
	}
	_, script, ok := strings.Cut(string(data), "RUN git init /src/libyang")
	if !ok {
		t.Fatal("Dockerfile source stage missing")
	}
	script, _, ok = strings.Cut(script, "\n\nFROM source AS build")
	if !ok {
		t.Fatal("Dockerfile build stage missing")
	}
	script = strings.ReplaceAll("git init /src/libyang"+script, "/src", dir)
	const git = `#!/bin/bash
set -euo pipefail
if [ "$1" = init ]; then mkdir -p "$2"; exit; fi
dir=$2; shift 2
case "$1" in
  fetch) [ "${FAKE_GIT_FAIL:-}" != fetch ] ;;
  checkout) ;;
  rev-parse)
    case "$dir" in
      */frr) printf '%s\n' "$FAKE_FRR_COMMIT" ;;
      */libyang) printf '%s\n' "$FAKE_LIBYANG_COMMIT" ;;
    esac ;;
  *) exit 20 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(git), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FRR_GIT_REF", "master")
	t.Setenv("LIBYANG_GIT_REF", "v2.1.148")
	t.Setenv("FAKE_FRR_COMMIT", strings.Repeat("d", 40))
	t.Setenv("FAKE_LIBYANG_COMMIT", strings.Repeat("e", 40))
	t.Setenv("FAKE_GIT_FAIL", "")
	if out, err := exec.CommandContext(t.Context(), "bash", "-euc", script).CombinedOutput(); err != nil {
		t.Fatalf("source stage failed: %v, %s", err, out)
	}
	revisions, err := os.ReadFile(filepath.Join(dir, "source-revisions"))
	if err != nil || string(revisions) != "FRR_COMMIT="+strings.Repeat("d", 40)+"\nLIBYANG_COMMIT="+strings.Repeat("e", 40)+"\n" {
		t.Fatalf("recorded revisions = %q, %v", revisions, err)
	}
	t.Setenv("FAKE_GIT_FAIL", "fetch")
	if _, err := exec.CommandContext(t.Context(), "bash", "-euc", script).CombinedOutput(); err == nil {
		t.Fatal("remote failure silently accepted")
	}
}

func TestManagerImageCacheRefreshAndFailurePolicy(t *testing.T) {
	t.Parallel()
	repo := imageTestRepo(t)
	inputs, err := imageInputsID(repo, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	oldID, newID := testImageID, strings.Repeat("b", 64)
	for _, tc := range []struct {
		name        string
		refresh     bool
		cached      bool
		guestID     string
		buildErr    error
		failCommand string
		mismatch    bool
		wantBuild   bool
		wantImport  bool
		wantTag     bool
		wantErr     bool
	}{
		{name: "offline guest", guestID: oldID},
		{name: "offline host", cached: true, wantImport: true, wantTag: true},
		{name: "matching guest wins over different host archive", cached: true, guestID: oldID},
		{name: "matching actual identity", cached: true, guestID: newID},
		{name: "explicit refresh bypasses both", refresh: true, cached: true, guestID: oldID, wantBuild: true, wantImport: true, wantTag: true},
		{name: "remote failure visible", refresh: true, guestID: oldID, buildErr: fmt.Errorf("remote fetch failed"), wantBuild: true, wantErr: true},
		{name: "failed transfer retains guest", refresh: true, guestID: oldID, failCommand: "transfer", wantBuild: true, wantErr: true},
		{name: "failed load retains guest", refresh: true, guestID: oldID, failCommand: "load", wantBuild: true, wantImport: true, wantErr: true},
		{name: "wrong identity retains guest", refresh: true, guestID: oldID, mismatch: true, wantBuild: true, wantImport: true, wantErr: true},
		{name: "failed tag retains guest", refresh: true, guestID: oldID, failCommand: "tag", wantBuild: true, wantImport: true, wantTag: true, wantErr: true},
		{name: "host unavailable guest path", refresh: true, guestID: oldID, buildErr: ErrHostDockerUnavailable, wantBuild: true, wantTag: true},
		{name: "guest build failure retains tag", refresh: true, guestID: oldID, buildErr: ErrHostDockerUnavailable, failCommand: "buildx", wantBuild: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			image := imageArchive(t.TempDir(), newID)
			cli := &fakeCLI{identities: map[string]imageIdentity{
				image.Tag: {ID: "sha256:" + newID, Inputs: inputs},
			}}
			if tc.guestID != "" {
				cli.identities[defaultGuestImage] = imageIdentity{ID: "sha256:" + tc.guestID, Inputs: inputs}
			}
			if tc.mismatch {
				cli.identities[image.Tag] = imageIdentity{ID: "sha256:" + oldID, Inputs: inputs}
			}
			if tc.failCommand == "transfer" {
				cli.transferErr = fmt.Errorf("transfer failed")
			}
			cli.execHook = func(spec ExecSpec) error {
				if tc.failCommand != "" && slices.Contains(spec.Command, tc.failCommand) {
					return fmt.Errorf("guest command failed")
				}
				return nil
			}
			builder := &fakeImageBuilder{result: image, err: tc.buildErr}
			if tc.cached {
				builder.cached = image
			}
			manager := NewManagerWithImageBuilder(cli, nil, builder)
			err := manager.ensureGuestImage(t.Context(), Environment{RepoDir: repo, GuestBinaryDir: "/stage"}, "amd64", tc.refresh)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ensure image error = %v, want error %v", err, tc.wantErr)
			}
			if (builder.buildCalls != 0) != tc.wantBuild {
				t.Fatalf("build calls = %d", builder.buildCalls)
			}
			var imported, tagged bool
			for _, spec := range cli.execs {
				imported = imported || slices.Contains(spec.Command, "load")
				tagged = tagged || slices.Contains(spec.Command, "tag")
			}
			if imported != tc.wantImport || tagged != tc.wantTag {
				t.Fatalf("import=%v tag=%v, want import=%v tag=%v", imported, tagged, tc.wantImport, tc.wantTag)
			}
			if tc.wantErr && cli.identities[defaultGuestImage].ID != "sha256:"+tc.guestID {
				t.Fatal("failed refresh changed the previous guest image")
			}
		})
	}
}

func TestImageIdentityFormatAllowsAbsentLabels(t *testing.T) {
	t.Parallel()
	format, err := template.New("inspect").Funcs(template.FuncMap{"json": func(v any) (string, error) {
		data, err := json.Marshal(v)
		return string(data), err
	}}).Parse(imageIdentityFormat)
	if err != nil {
		t.Fatal(err)
	}
	for _, labels := range []any{nil, map[string]string{}, map[string]string{guestImageInputsLabel: "inputs"}} {
		var output bytes.Buffer
		if err := format.Execute(&output, map[string]any{"Id": "sha256:" + testImageID, "Config": map[string]any{"Labels": labels}}); err != nil {
			t.Fatal(err)
		}
		if _, err := parseImageIdentity(output.String()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExplicitImageRefreshRequiresDockerfile(t *testing.T) {
	t.Parallel()
	manager := NewManager(nil, nil)
	for _, refresh := range []bool{false, true} {
		err := manager.ensureGuestImage(t.Context(), Environment{RepoDir: t.TempDir()}, "amd64", refresh)
		if (err != nil) != refresh {
			t.Fatalf("missing Dockerfile: refresh=%v, error=%v", refresh, err)
		}
	}
}

func TestGuestImageBuildAndLookupWithMockSystemCLI(t *testing.T) {
	for _, failure := range []string{"", "build", "lookup"} {
		t.Run("failure="+failure, func(t *testing.T) {
			log := fakeHostDocker(t)
			for name, script := range map[string]string{
				"multipass": "#!/bin/bash\nexec /bin/bash\n",
				"sudo":      "#!/bin/bash\nexec \"$@\"\n",
				// A host login shell would reset PATH and escape the mocks.
				"bash": "#!/bin/sh\nif [ \"$1\" = -lc ]; then shift; exec /bin/bash -c \"$@\"; fi\nexec /bin/bash \"$@\"\n",
			} {
				if err := os.WriteFile(filepath.Join(filepath.Dir(log), name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("FAKE_DOCKER_FAIL", failure)
			t.Setenv("FRR_GIT_REF", "refs/pull/123/head")
			repo := imageTestRepo(t)
			manager := NewManagerWithImageBuilder(newSystemCLI(), nil, &fakeImageBuilder{err: ErrHostDockerUnavailable})
			err := manager.ensureGuestImage(t.Context(), Environment{RepoDir: repo, GuestRepoDir: repo, GuestHostDir: repo, GuestBinaryDir: t.TempDir()}, "amd64", true)
			if (err != nil) != (failure != "") {
				t.Fatalf("guest error = %v, failure = %q", err, failure)
			}
			calls, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(calls), "image ls --format") {
				t.Fatal("guest lookup escaped the Docker mock")
			}
			tagged := strings.Contains(string(calls), "image tag sha256:"+testImageID+" "+defaultGuestImage)
			if tagged != (failure == "") {
				t.Fatalf("guest tag published=%v on failure=%q", tagged, failure)
			}
			if failure == "" && (!strings.Contains(string(calls), "FRR_GIT_REF=refs/pull/123/head") || !strings.Contains(string(calls), "--no-cache-filter source")) {
				t.Fatal("guest build omitted refs or source materialization")
			}
		})
	}
}

func TestImageBuildIDIncludesDockerInputs(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repoDir, "docker", "frr"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(repoDir, "Dockerfile"):                    "FROM scratch\nCOPY docker/frr/docker-start /usr/lib/frr/docker-start\n",
		filepath.Join(repoDir, ".dockerignore"):                 ".git\n.build\n",
		filepath.Join(repoDir, "docker", "frr", "docker-start"): "#!/bin/sh\necho one\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	first, err := imageBuildID(repoDir, "amd64")
	if err != nil {
		t.Fatalf("imageBuildID(first) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "docker", "frr", "docker-start"), []byte("#!/bin/sh\necho two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(docker-start) error = %v", err)
	}
	second, err := imageBuildID(repoDir, "amd64")
	if err != nil {
		t.Fatalf("imageBuildID(second) error = %v", err)
	}
	if first == second {
		t.Fatalf("imageBuildID() = %q after docker input change, want build ID to change", second)
	}
}
