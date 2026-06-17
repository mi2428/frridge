package multipass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceDigestIncludesEmbeddedAssets(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repoDir, "cmd", "frridge"),
		filepath.Join(repoDir, "internal", "multipass"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(repoDir, "go.mod"):                                        "module example.com/frridge\n\ngo 1.25.0\n",
		filepath.Join(repoDir, "cmd", "frridge", "main.go"):                     "package main\n\nfunc main() {}\n",
		filepath.Join(repoDir, "internal", "multipass", "cloud-init.yaml.tmpl"): "#cloud-config\n",
		filepath.Join(repoDir, "internal", "multipass", "guest-bootstrap.sh"):   "#!/bin/sh\necho one\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	first, err := sourceDigest(repoDir)
	if err != nil {
		t.Fatalf("sourceDigest(first) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "internal", "multipass", "guest-bootstrap.sh"), []byte("#!/bin/sh\necho two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(guest-bootstrap.sh) error = %v", err)
	}
	second, err := sourceDigest(repoDir)
	if err != nil {
		t.Fatalf("sourceDigest(second) error = %v", err)
	}
	if first == second {
		t.Fatalf("sourceDigest() = %q after embedded asset change, want digest to change", second)
	}
}
