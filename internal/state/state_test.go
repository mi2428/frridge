package state

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWriteReadAndScanWorkDir(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	statePath := filepath.Join(workDir, "labs", "lab-a", "state.json")
	want := LabState{
		Lab:            "lab-a",
		TopologyDigest: "sha256:" + strings.Repeat("0", 64),
		OwnedBridges:   map[string]string{"lan": BridgeName("lab-a", "lan")},
		Containers: map[string]ContainerState{
			"r1": {ID: "abc", PID: 123, ConfigDir: "/tmp/r1"},
		},
	}

	if err := Write(statePath, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(statePath)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("Read() = %+v, want %+v", got, want)
	}

	snapshots, err := ScanWorkDir(workDir)
	if err != nil {
		t.Fatalf("ScanWorkDir() error = %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("len(ScanWorkDir()) = %d, want 1", len(snapshots))
	}
	if snapshots[0].State.Lab != want.Lab {
		t.Fatalf("ScanWorkDir()[0].State.Lab = %q, want %q", snapshots[0].State.Lab, want.Lab)
	}
}

func TestWriteAtomicReplacementAndFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	snapshot := LabState{Lab: "atomic", TopologyDigest: "sha256:" + strings.Repeat("1", 64), Containers: map[string]ContainerState{}, OwnedBridges: map[string]string{}}
	if err := Write(path, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	snapshot.OwnedBridges["lan"] = BridgeName(snapshot.Lab, "lan")
	if err := Write(path, snapshot); err != nil {
		t.Fatal(err)
	}
	oldInfo, err := old.Stat()
	if err != nil {
		t.Fatal(err)
	}
	newInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(oldInfo, newInfo) || newInfo.Mode().Perm() != 0o640 {
		t.Fatal("state was modified in place or permissions changed")
	}
	got, err := Read(path)
	if err != nil || !reflect.DeepEqual(*got, snapshot) {
		t.Fatalf("replacement snapshot=%v err=%v", got, err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(blocked, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(blocked, snapshot); err == nil {
		t.Fatal("replacement of a directory unexpectedly succeeded")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("failed replacement lost existing data: %q %v", data, err)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".state-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary files remain: %v %v", temps, err)
	}
}

func TestReadRejectsMalformedStateAndForeignInventory(t *testing.T) {
	t.Parallel()
	valid := `{"lab":"lab-a","topologyDigest":"sha256:` + strings.Repeat("0", 64) + `","containers":{"r1":{"id":"abc","pid":123,"configDir":"config"}}}`
	for _, data := range []string{
		`null`, `{}`, valid + `{}`, strings.Replace(valid, `"lab-a"`, `"../foreign"`, 1),
		strings.Replace(valid, `"sha256:`, `"unknown:`, 1),
		strings.Replace(valid, `"abc"`, `""`, 1), strings.Replace(valid, `123`, `-1`, 1),
		strings.TrimSuffix(valid, "}") + `,"unknown":true}`,
		strings.TrimSuffix(valid, "}") + `,"ownedBridges":{"lan":"unowned0"}}`,
		strings.TrimSuffix(valid, "}") + `,"ownedBridges":{"lan":"` + BridgeName("lab-b", "lan") + `"}}`,
	} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Fatal("malformed state was accepted")
		}
	}
	workDir := t.TempDir()
	path := filepath.Join(workDir, "labs", "lab-b", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanWorkDir(workDir); err == nil {
		t.Fatal("foreign lab in fileless console scan was accepted")
	}
	legacyPath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(legacyPath, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := Read(legacyPath); err != nil || snapshot.OwnedBridges != nil {
		t.Fatalf("legacy snapshot without new field is unreadable: %v %v", snapshot, err)
	}
}
