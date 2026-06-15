package state

import (
	"path/filepath"
	"testing"
)

func TestWriteReadAndScanWorkDir(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	statePath := filepath.Join(workDir, "labs", "lab-a", "state.json")
	want := LabState{
		Lab:            "lab-a",
		TopologyDigest: "sha256:test",
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
	if got.Lab != want.Lab || got.TopologyDigest != want.TopologyDigest {
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
