package multipass

import (
	"os"
	"path/filepath"
	"testing"
)

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
