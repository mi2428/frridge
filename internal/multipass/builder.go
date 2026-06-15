package multipass

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// GoBuilder cross-compiles the Linux frridge binary on the host and caches the
// result below the user cache directory.
type GoBuilder struct {
	cacheDir string
}

// NewGoBuilder creates a GoBuilder rooted at cacheDir. When cacheDir is empty,
// the platform user cache directory is used.
func NewGoBuilder(cacheDir string) (*GoBuilder, error) {
	if strings.TrimSpace(cacheDir) == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user cache dir: %w", err)
		}
		cacheDir = filepath.Join(userCacheDir, "frridge-mp")
	}
	return &GoBuilder{cacheDir: cacheDir}, nil
}

// Build returns a cached Linux build keyed by the current non-test Go sources
// and the requested target architecture.
func (b *GoBuilder) Build(ctx context.Context, repoDir, goarch string) (BuildResult, error) {
	digest, err := sourceDigest(repoDir)
	if err != nil {
		return BuildResult{}, err
	}

	id := digest + "-" + goarch
	outputDir := filepath.Join(b.cacheDir, "bin", id)
	outputPath := filepath.Join(outputDir, "frridge")
	if _, err := os.Stat(outputPath); err == nil {
		return BuildResult{ID: id, Path: outputPath}, nil
	} else if err != nil && !os.IsNotExist(err) {
		return BuildResult{}, fmt.Errorf("stat cached binary: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("create cache dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(outputDir, "frridge-*")
	if err != nil {
		return BuildResult{}, fmt.Errorf("create temp binary: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return BuildResult{}, fmt.Errorf("close temp binary: %w", err)
	}
	defer os.Remove(tmpPath)

	cmd := exec.CommandContext(ctx, "go", "build", "-o", tmpPath, "./cmd/frridge")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+goarch,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return BuildResult{}, fmt.Errorf("build guest frridge: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		return BuildResult{}, fmt.Errorf("install cached binary: %w", err)
	}
	if err := os.Chmod(outputPath, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("chmod cached binary: %w", err)
	}

	return BuildResult{ID: id, Path: outputPath}, nil
}

func sourceDigest(repoDir string) (string, error) {
	files, err := sourceFiles(repoDir)
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

func sourceFiles(repoDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(repoDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".build", ".frridge":
				return filepath.SkipDir
			}
			return nil
		}

		name := entry.Name()
		switch {
		case name == "go.mod", name == "go.sum":
			files = append(files, path)
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk source tree: %w", err)
	}

	slices.Sort(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go sources found below %q", repoDir)
	}
	return files, nil
}
