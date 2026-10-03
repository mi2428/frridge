package multipass

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	defaultHostImageCacheTagPrefix = "frridge-mp-cache"
	guestImageBuildIDLabel         = "io.frridge.build-id"
	guestImageInputsLabel          = "io.frridge.build-inputs"
)

var (
	// ErrHostDockerUnavailable reports that host Docker cannot be used to build
	// or export the companion image.
	ErrHostDockerUnavailable = errors.New("host docker unavailable")

	imageDigestRequiredFiles = []string{
		"Dockerfile",
	}
	imageDigestOptionalFiles = []string{
		".dockerignore",
	}
	imageDigestOptionalDirs = []string{
		"docker",
	}
)

// ImageBuilder produces cached companion image archives on the host.
type ImageBuilder interface {
	Cached(repoDir, goarch string) (ImageBuildResult, error)
	Build(ctx context.Context, repoDir, goarch string) (ImageBuildResult, error)
}

// ImageBuildResult identifies a cached Docker image archive on the host.
type ImageBuildResult struct {
	ID     string
	Path   string
	Tag    string
	Legacy bool
}

// DockerImageBuilder builds the repo's companion image on the host with Docker
// and stores a reusable archive below the user cache directory.
type DockerImageBuilder struct {
	cacheDir string
}

// NewDockerImageBuilder creates a DockerImageBuilder rooted at cacheDir. When
// cacheDir is empty, the platform user cache directory is used.
func NewDockerImageBuilder(cacheDir string) (*DockerImageBuilder, error) {
	if strings.TrimSpace(cacheDir) == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user cache dir: %w", err)
		}
		cacheDir = filepath.Join(userCacheDir, "frridge-mp")
	}
	return &DockerImageBuilder{cacheDir: cacheDir}, nil
}

// Cached looks up local metadata without contacting Docker or upstream. Legacy
// archives remain reusable, but their source revisions are unknown until refresh.
func (b *DockerImageBuilder) Cached(repoDir, goarch string) (ImageBuildResult, error) {
	inputs, err := imageInputsID(repoDir, goarch)
	if err != nil {
		return ImageBuildResult{}, err
	}
	outputDir := filepath.Join(b.cacheDir, "images")
	data, err := os.ReadFile(filepath.Join(outputDir, inputs+".id"))
	if err == nil {
		id, err := parseDockerImageID(strings.TrimSpace(string(data)))
		if err != nil {
			return ImageBuildResult{}, fmt.Errorf("read cached companion image identity: %w", err)
		}
		result := imageArchive(outputDir, id)
		if err := checkImageArchive(result.Path); err != nil {
			return ImageBuildResult{}, err
		}
		return result, nil
	}
	if !os.IsNotExist(err) {
		return ImageBuildResult{}, fmt.Errorf("read cached companion image metadata: %w", err)
	}
	if !noImageRefOverrides() {
		return ImageBuildResult{}, nil
	}
	legacyID, err := imageBuildID(repoDir, goarch)
	if err != nil {
		return ImageBuildResult{}, err
	}
	result := imageArchive(outputDir, legacyID)
	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		return ImageBuildResult{}, nil
	}
	if err := checkImageArchive(result.Path); err != nil {
		return ImageBuildResult{}, err
	}
	result.Legacy = true
	return result, nil
}

// Build materializes current upstream refs, then publishes an archive keyed by
// the actual Docker image ID. Call Cached first when refresh was not requested.
func (b *DockerImageBuilder) Build(ctx context.Context, repoDir, goarch string) (ImageBuildResult, error) {
	inputs, err := imageInputsID(repoDir, goarch)
	if err != nil {
		return ImageBuildResult{}, err
	}
	if !hostDockerAvailable(ctx) {
		if err := ctx.Err(); err != nil {
			return ImageBuildResult{}, err
		}
		return ImageBuildResult{}, ErrHostDockerUnavailable
	}
	platform, err := dockerPlatform(goarch)
	if err != nil {
		return ImageBuildResult{}, err
	}
	outputDir := filepath.Join(b.cacheDir, "images")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return ImageBuildResult{}, fmt.Errorf("create image cache dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(outputDir, ".refresh-*.tar")
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("create temp image archive: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return ImageBuildResult{}, fmt.Errorf("close temp image archive: %w", err)
	}
	defer os.Remove(tmpPath)
	iidPath := tmpPath + ".id"
	defer os.Remove(iidPath)

	if err := buildHostImage(ctx, repoDir, platform, inputs, iidPath); err != nil {
		return ImageBuildResult{}, err
	}
	data, err := os.ReadFile(iidPath)
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("read built companion image identity: %w", err)
	}
	id, err := parseDockerImageID(strings.TrimSpace(string(data)))
	if err != nil {
		return ImageBuildResult{}, err
	}
	info, err := inspectHostImage(ctx, "sha256:"+id)
	if err != nil {
		return ImageBuildResult{}, err
	}
	if info.ID != "sha256:"+id || info.Inputs != inputs {
		return ImageBuildResult{}, fmt.Errorf("built companion image identity or inputs mismatch")
	}
	result := imageArchive(outputDir, id)
	if err := runHostDocker(ctx, "tag companion image", "image", "tag", info.ID, result.Tag); err != nil {
		return ImageBuildResult{}, err
	}
	if err := saveHostImage(ctx, result.Tag, tmpPath); err != nil {
		return ImageBuildResult{}, err
	}
	if err := checkImageArchive(tmpPath); err != nil {
		return ImageBuildResult{}, err
	}
	if err := os.Rename(tmpPath, result.Path); err != nil {
		return ImageBuildResult{}, fmt.Errorf("install cached companion image archive: %w", err)
	}
	// Publish the lookup only after the complete archive is installed. A failed
	// refresh leaves the previous lookup/archive available to offline callers.
	if err := os.Rename(iidPath, filepath.Join(outputDir, inputs+".id")); err != nil {
		return ImageBuildResult{}, fmt.Errorf("install companion image metadata: %w", err)
	}
	return result, nil
}

func imageArchive(dir, id string) ImageBuildResult {
	return ImageBuildResult{ID: id, Path: filepath.Join(dir, id+".tar"), Tag: hostImageTag(id)}
}

func checkImageArchive(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat cached companion image archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("companion image archive is not a nonempty regular file")
	}
	return nil
}

func imageRefs() (string, string) {
	return os.Getenv("FRR_GIT_REF"), os.Getenv("LIBYANG_GIT_REF")
}

func noImageRefOverrides() bool {
	frr, libyang := imageRefs()
	return frr == "" && libyang == ""
}

func imageInputsID(repoDir, goarch string) (string, error) {
	if _, err := dockerPlatform(goarch); err != nil {
		return "", err
	}
	local, err := imageBuildID(repoDir, goarch)
	if err != nil {
		return "", err
	}
	frr, libyang := imageRefs()
	sum := sha256.Sum256([]byte(local + "\x00" + frr + "\x00" + libyang))
	return "v2-" + hex.EncodeToString(sum[:]), nil
}

type imageIdentity struct {
	ID       string
	Inputs   string
	LegacyID string
}

const imageIdentityFormat = `{"ID":{{json .Id}}{{with .Config.Labels}},"Inputs":{{json (index . "` + guestImageInputsLabel + `")}},"LegacyID":{{json (index . "` + guestImageBuildIDLabel + `")}}{{end}}}`

func parseImageIdentity(output string) (imageIdentity, error) {
	var info imageIdentity
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		return info, fmt.Errorf("decode companion image identity: %w", err)
	}
	if _, err := parseDockerImageID(info.ID); err != nil {
		return info, err
	}
	return info, nil
}

func parseDockerImageID(value string) (string, error) {
	id, ok := strings.CutPrefix(value, "sha256:")
	if !ok || len(id) != 64 {
		return "", fmt.Errorf("invalid companion Docker image ID")
	}
	if _, err := hex.DecodeString(id); err != nil || id != strings.ToLower(id) {
		return "", fmt.Errorf("invalid companion Docker image ID")
	}
	return id, nil
}

func imageBuildID(repoDir, goarch string) (string, error) {
	digest, err := imageContextDigest(repoDir)
	if err != nil {
		return "", err
	}
	return digest + "-" + goarch, nil
}

func imageContextDigest(repoDir string) (string, error) {
	files, err := imageContextFiles(repoDir)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	for _, file := range files {
		rel, err := filepath.Rel(repoDir, file)
		if err != nil {
			return "", fmt.Errorf("rel path for %q: %w", file, err)
		}
		if _, err := io.WriteString(hash, filepath.ToSlash(rel)); err != nil {
			return "", fmt.Errorf("hash path %q: %w", rel, err)
		}
		if _, err := io.WriteString(hash, "\x00"); err != nil {
			return "", fmt.Errorf("hash separator for %q: %w", rel, err)
		}

		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %q: %w", rel, err)
		}
		if _, err := hash.Write(data); err != nil {
			return "", fmt.Errorf("hash file %q: %w", rel, err)
		}
		if _, err := io.WriteString(hash, "\x00"); err != nil {
			return "", fmt.Errorf("hash separator for %q: %w", rel, err)
		}
	}

	return hex.EncodeToString(hash.Sum(nil))[:16], nil
}

func imageContextFiles(repoDir string) ([]string, error) {
	files := make([]string, 0, len(imageDigestRequiredFiles)+len(imageDigestOptionalFiles))
	seen := make(map[string]struct{}, len(imageDigestRequiredFiles)+len(imageDigestOptionalFiles))

	addFile := func(path string) error {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("%q is a directory", path)
		}
		if _, ok := seen[path]; ok {
			return nil
		}
		seen[path] = struct{}{}
		files = append(files, path)
		return nil
	}

	for _, rel := range imageDigestRequiredFiles {
		if err := addFile(filepath.Join(repoDir, rel)); err != nil {
			return nil, fmt.Errorf("collect companion image input %q: %w", rel, err)
		}
	}
	for _, rel := range imageDigestOptionalFiles {
		path := filepath.Join(repoDir, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("collect companion image input %q: %w", rel, err)
		}
		if err := addFile(path); err != nil {
			return nil, fmt.Errorf("collect companion image input %q: %w", rel, err)
		}
	}
	for _, rel := range imageDigestOptionalDirs {
		root := filepath.Join(repoDir, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("collect companion image input %q: %w", rel, err)
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			return addFile(path)
		})
		if err != nil {
			return nil, fmt.Errorf("walk companion image input %q: %w", rel, err)
		}
	}

	slices.Sort(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no companion image build inputs found below %q", repoDir)
	}
	return files, nil
}

func hostDockerAvailable(ctx context.Context) bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}

	for _, args := range [][]string{
		{"version", "--format", "{{.Server.Version}}"},
		{"buildx", "version"},
	} {
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return false
		}
	}
	return true
}

func imageBuildArgs(inputs, iidPath string) []string {
	frr, libyang := imageRefs()
	args := []string{
		"buildx",
		"build",
		"--load",
		"--no-cache-filter", "source",
		"--iidfile", iidPath,
		"--label", guestImageInputsLabel + "=" + inputs,
	}
	// Let Docker apply the checkout's ARG defaults; only explicit overrides
	// should replace a custom Dockerfile's pinned refs.
	if frr != "" {
		args = append(args, "--build-arg", "FRR_GIT_REF="+frr)
	}
	if libyang != "" {
		args = append(args, "--build-arg", "LIBYANG_GIT_REF="+libyang)
	}
	return append(args, ".")
}

func buildHostImage(ctx context.Context, repoDir, platform, inputs, iidPath string) error {
	args := append([]string{"--platform", platform}, imageBuildArgs(inputs, iidPath)[2:]...)
	args = append([]string{"buildx", "build"}, args...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = repoDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build companion image on host: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func inspectHostImage(ctx context.Context, image string) (imageIdentity, error) {
	output, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", imageIdentityFormat, image).CombinedOutput()
	if err != nil {
		return imageIdentity{}, fmt.Errorf("inspect built companion image: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return parseImageIdentity(string(output))
}

func runHostDocker(ctx context.Context, action string, args ...string) error {
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", action, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func saveHostImage(ctx context.Context, tag, archivePath string) error {
	return runHostDocker(ctx, "export companion image archive on host", "save", "-o", archivePath, tag)
}

func dockerPlatform(goarch string) (string, error) {
	switch strings.TrimSpace(goarch) {
	case "amd64":
		return "linux/amd64", nil
	case "arm64":
		return "linux/arm64", nil
	default:
		return "", fmt.Errorf("unsupported guest architecture %q", strings.TrimSpace(goarch))
	}
}

func hostImageTag(id string) string {
	return defaultHostImageCacheTagPrefix + ":" + id
}
