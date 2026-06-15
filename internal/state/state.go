// Package state persists the minimal runtime snapshot needed to find and tear
// down a lab after it has been created.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LabState is the on-disk snapshot for one running lab.
type LabState struct {
	Lab            string                    `json:"lab"`
	TopologyDigest string                    `json:"topologyDigest"`
	Containers     map[string]ContainerState `json:"containers"`
}

// ContainerState stores the runtime identifiers needed to reattach and clean
// up one container.
type ContainerState struct {
	ID        string `json:"id"`
	PID       int    `json:"pid"`
	ConfigDir string `json:"configDir"`
}

// Snapshot couples a state file path with its decoded content.
type Snapshot struct {
	Path  string
	State LabState
}

// Read loads one LabState from disk.
func Read(path string) (*LabState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var snapshot LabState
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode state file: %w", err)
	}
	return &snapshot, nil
}

// Write stores a LabState as pretty-printed JSON.
func Write(path string, snapshot LabState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

// Remove deletes one state file if it exists.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state file: %w", err)
	}
	return nil
}

// ScanWorkDir finds every state file below the default lab workspace layout.
func ScanWorkDir(workDir string) ([]Snapshot, error) {
	matches, err := filepath.Glob(filepath.Join(workDir, "labs", "*", "state.json"))
	if err != nil {
		return nil, fmt.Errorf("glob state files: %w", err)
	}

	snapshots := make([]Snapshot, 0, len(matches))
	for _, path := range matches {
		snapshot, err := Read(path)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, Snapshot{
			Path:  path,
			State: *snapshot,
		})
	}

	return snapshots, nil
}
