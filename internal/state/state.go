// Package state persists the minimal runtime snapshot needed to find and tear
// down a lab after it has been created.
package state

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LabState is the on-disk snapshot for one running lab.
type LabState struct {
	Lab            string                    `json:"lab"`
	TopologyDigest string                    `json:"topologyDigest"`
	Containers     map[string]ContainerState `json:"containers"`
	// Nil denotes a legacy snapshot; an empty map is a complete inventory.
	OwnedBridges map[string]string `json:"ownedBridges"`
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode state file: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("state file must contain one JSON object")
	}
	if err := snapshot.Validate(snapshot.Lab); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// Validate rejects malformed snapshots and inventory not scoped to lab.
// Legacy snapshots are readable, but cannot be used for network cleanup.
func (s LabState) Validate(lab string) error {
	if strings.TrimSpace(s.Lab) == "" || s.Lab == "." || s.Lab == ".." || filepath.Base(s.Lab) != s.Lab || strings.ContainsRune(s.Lab, '\x00') || s.Lab != lab {
		return fmt.Errorf("state lab does not match lab %q", lab)
	}
	digestText, hasPrefix := strings.CutPrefix(s.TopologyDigest, "sha256:")
	if digest, err := hex.DecodeString(digestText); !hasPrefix || err != nil || len(digest) != 32 {
		return fmt.Errorf("state has invalid topology digest")
	}
	if s.Containers == nil {
		return fmt.Errorf("state has no container inventory")
	}
	for router, container := range s.Containers {
		if strings.TrimSpace(router) == "" || strings.TrimSpace(container.ID) == "" || container.PID < 0 {
			return fmt.Errorf("state has invalid container entry")
		}
	}
	for link, name := range s.OwnedBridges {
		if strings.TrimSpace(link) == "" || name != BridgeName(s.Lab, link) {
			return fmt.Errorf("state has foreign or malformed owned bridge inventory")
		}
	}
	return nil
}

// BridgeName is the deterministic host bridge name for one lab/link pair.
func BridgeName(lab, link string) string {
	sum := sha1.Sum([]byte(lab + "/" + link))
	return "frb" + fmt.Sprintf("%x", sum)[:11]
}

// Write atomically replaces a LabState with pretty-printed JSON.
func Write(path string, snapshot LabState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat state file: %w", err)
	}
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("set state file permissions: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync state file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close state file: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
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
		if err := snapshot.Validate(filepath.Base(filepath.Dir(path))); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, Snapshot{
			Path:  path,
			State: *snapshot,
		})
	}

	return snapshots, nil
}
