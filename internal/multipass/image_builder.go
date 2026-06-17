package multipass

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Build(ctx context.Context, repoDir, goarch string) (ImageBuildResult, error)
}

// ImageBuildResult identifies a cached Docker image archive on the host.
type ImageBuildResult struct {
	ID   string
	Path string
	Tag  string
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

// Build returns a cached companion image archive keyed by the Docker build
// context and requested guest architecture.
func (b *DockerImageBuilder) Build(ctx context.Context, repoDir, goarch string) (ImageBuildResult, error) {
	id, err := imageBuildID(repoDir, goarch)
	if err != nil {
		return ImageBuildResult{}, err
	}

	outputDir := filepath.Join(b.cacheDir, "images")
	outputPath := filepath.Join(outputDir, id+".tar")
	result := ImageBuildResult{
		ID:   id,
		Path: outputPath,
		Tag:  hostImageTag(id),
	}
	if _, err := os.Stat(outputPath); err == nil {
		return result, nil
	} else if err != nil && !os.IsNotExist(err) {
		return ImageBuildResult{}, fmt.Errorf("stat cached companion image archive: %w", err)
	}

	if !hostDockerAvailable(ctx) {
		return ImageBuildResult{}, ErrHostDockerUnavailable
	}

	platform, err := dockerPlatform(goarch)
	if err != nil {
		return ImageBuildResult{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return ImageBuildResult{}, fmt.Errorf("create image cache dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(outputDir, id+"-*.tar")
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("create temp image archive: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return ImageBuildResult{}, fmt.Errorf("close temp image archive: %w", err)
	}
	defer os.Remove(tmpPath)

	if err := buildHostImage(ctx, repoDir, platform, result.Tag, result.ID); err != nil {
		return ImageBuildResult{}, err
	}
	if err := saveHostImage(ctx, result.Tag, tmpPath); err != nil {
		return ImageBuildResult{}, err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return ImageBuildResult{}, fmt.Errorf("install cached companion image archive: %w", err)
	}

	return result, nil
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

func buildHostImage(ctx context.Context, repoDir, platform, tag, id string) error {
	args := []string{
		"buildx",
		"build",
		"--load",
		"--platform", platform,
		"--label", guestImageBuildIDLabel + "=" + id,
		"-t", tag,
		".",
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = repoDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build companion image on host: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func saveHostImage(ctx context.Context, tag, archivePath string) error {
	cmd := exec.CommandContext(ctx, "docker", "save", "-o", archivePath, tag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("export companion image archive on host: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
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
