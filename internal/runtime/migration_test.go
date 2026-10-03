package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"frridge/internal/config"
	"frridge/internal/state"
)

func legacyStateFixture(t *testing.T) (string, *config.Topology, state.LabState, string) {
	t.Helper()
	path := startupTopologyFile(t)
	topology, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := topology.Digest()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := state.LabState{Lab: topology.Lab.Name, TopologyDigest: digest, Containers: map[string]state.ContainerState{
		"r1": {ID: "original-r1", PID: 123, ConfigDir: topology.RouterConfigDir("r1")},
		"r2": {ID: "original-r2", PID: 456, ConfigDir: topology.RouterConfigDir("r2")},
	}}
	if err := state.Write(topology.StatePath(), snapshot); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(topology.RouterConfigDir("r1"), "frr.conf")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{configPath, topology.RouterSeedMarkerPath("r1")} {
		if err := os.WriteFile(file, []byte("preserve original\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path, topology, snapshot, configPath
}

func TestMigrateLegacyStatePreservesExistingStateAndConfig(t *testing.T) {
	t.Parallel()
	path, topology, original, configPath := legacyStateFixture(t)
	manager := NewManager(nil, nil) // Migration must not contact Docker or netlink.
	if id, err := manager.lookupRouterContainer(context.Background(), topology, "r1"); err != nil || id != original.Containers["r1"].ID {
		t.Fatalf("legacy CLI lookup: id=%q err=%v", id, err)
	}
	if err := manager.MigrateState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	got, err := state.Read(topology.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.OwnedBridges, map[string]string{"lan": bridgeLinkName(topology.Lab.Name, "lan")}) {
		t.Fatalf("inventory = %v", got.OwnedBridges)
	}
	got.OwnedBridges = nil
	if !reflect.DeepEqual(*got, original) {
		t.Fatalf("migration changed legacy fields: got=%v want=%v", got, original)
	}
	for _, file := range []string{configPath, topology.RouterSeedMarkerPath("r1")} {
		if data, err := os.ReadFile(file); err != nil || string(data) != "preserve original\n" {
			t.Fatalf("migration changed config/seed marker: %q %v", data, err)
		}
	}
	before, err := os.ReadFile(topology.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.MigrateState(context.Background(), path); err == nil {
		t.Fatal("modern state was re-migrated")
	}
	if after, err := os.ReadFile(topology.StatePath()); err != nil || !slices.Equal(after, before) {
		t.Fatal("rejected migration changed state")
	}
}

func TestMigrationUsesWorkDirOverrideWithoutChangingOriginalMetadata(t *testing.T) {
	path, topology, original, configPath := legacyStateFixture(t)
	originalStatePath := topology.StatePath()
	before, err := os.ReadFile(originalStatePath)
	if err != nil {
		t.Fatal(err)
	}
	override := t.TempDir()
	selectedStatePath := filepath.Join(override, "labs", topology.Lab.Name, "state.json")
	if err := state.Write(selectedStatePath, original); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.WorkDirOverrideEnv, override)
	if err := NewManager(nil, nil).MigrateState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	got, err := state.Read(selectedStatePath)
	if err != nil || len(got.OwnedBridges) != 1 {
		t.Fatalf("selected workspace was not migrated: %v %v", got, err)
	}
	got.OwnedBridges = nil
	if !reflect.DeepEqual(*got, original) {
		t.Fatal("workspace override changed original metadata")
	}
	if after, err := os.ReadFile(originalStatePath); err != nil || !slices.Equal(after, before) {
		t.Fatal("migration modified the unselected workspace")
	}
	if data, err := os.ReadFile(configPath); err != nil || string(data) != "preserve original\n" {
		t.Fatal("migration modified existing configuration")
	}
}

func TestMigrationRequiresExactOriginalAndValidState(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"pings", "lab", "digest", "malformed-state", "foreign-inventory", "malformed-original", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			path, topology, snapshot, configPath := legacyStateFixture(t)
			ctx := context.Background()
			switch failure {
			case "pings", "malformed-original":
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				text := "pings:\n  - name: added\n    from: {router: r1}\n    to: 192.0.2.1\n"
				if failure == "malformed-original" {
					text = "unknown: true\n"
				}
				if _, err := file.WriteString(text); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			case "lab":
				snapshot.Lab = "another-lab"
			case "digest":
				snapshot.TopologyDigest = "sha256:" + strings.Repeat("0", 64)
			case "foreign-inventory":
				snapshot.OwnedBridges = map[string]string{"lan": bridgeLinkName("another-lab", "lan")}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := state.Write(topology.StatePath(), snapshot); err != nil {
				t.Fatal(err)
			}
			if failure == "malformed-state" {
				if err := os.WriteFile(topology.StatePath(), []byte("{broken"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(topology.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			if err := NewManager(nil, nil).MigrateState(ctx, path); err == nil {
				t.Fatalf("migration accepted %s", failure)
			}
			if after, err := os.ReadFile(topology.StatePath()); err != nil || !slices.Equal(after, before) {
				t.Fatalf("failed migration modified snapshot: %v", err)
			}
			if data, err := os.ReadFile(configPath); err != nil || string(data) != "preserve original\n" {
				t.Fatalf("failed migration modified config: %q %v", data, err)
			}
		})
	}
}

func TestMigrationRecordsEmptyInventoryForOriginalWithoutBridges(t *testing.T) {
	t.Parallel()
	path := startupTopologyFile(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Split(string(data), "links:")[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	topology, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := topology.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Write(topology.StatePath(), state.LabState{Lab: topology.Lab.Name, TopologyDigest: digest, Containers: map[string]state.ContainerState{"r1": {ID: "r1"}, "r2": {ID: "r2"}}}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(&fakeDockerClient{}, &fakeNetworkManager{})
	if err := manager.MigrateState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.Read(topology.StatePath())
	if err != nil || snapshot.OwnedBridges == nil || len(snapshot.OwnedBridges) != 0 {
		t.Fatalf("empty modern inventory lost: %v %v", snapshot, err)
	}
	if err := manager.cleanupRuntime(context.Background(), topology, nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupDoesNotInferMissingInventoryFromCurrentYAML(t *testing.T) {
	t.Parallel()
	for _, orphan := range []bool{false, true} {
		path, topology, _, configPath := legacyStateFixture(t)
		if err := state.Remove(topology.StatePath()); err != nil {
			t.Fatal(err)
		}
		fakeDocker := &fakeDockerClient{listFn: func(context.Context, map[string]string) ([]string, error) {
			if orphan {
				return []string{"orphan"}, nil
			}
			return nil, nil
		}}
		fakeNetwork := &fakeNetworkManager{}
		manager := NewManager(fakeDocker, fakeNetwork)
		err := manager.cleanupRuntime(context.Background(), topology, nil, false)
		if (orphan && err == nil) || (!orphan && err != nil) || len(fakeDocker.removed) != 0 || len(fakeNetwork.ops) != 0 {
			t.Fatalf("missing inventory cleanup: orphan=%t err=%v removed=%v ops=%v", orphan, err, fakeDocker.removed, fakeNetwork.ops)
		}
		if orphan {
			if err := manager.up(context.Background(), path, UpOptions{Recreate: true, Reseed: true}); err == nil {
				t.Fatal("recreate without an inventory was accepted")
			}
			if len(fakeDocker.removed) != 0 || len(fakeNetwork.ops) != 0 {
				t.Fatal("recreate changed runtime before rejecting missing inventory")
			}
			if _, err := os.Stat(topology.StatePath()); !os.IsNotExist(err) {
				t.Fatal("rejected recreate created a snapshot")
			}
			if err := manager.MigrateState(context.Background(), path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("original YAML alone recovered missing state: %v", err)
			}
		}
		for _, file := range []string{configPath, topology.RouterSeedMarkerPath("r1")} {
			if data, err := os.ReadFile(file); err != nil || string(data) != "preserve original\n" {
				t.Fatal("missing-inventory operation changed config/seed marker")
			}
		}
	}
}

func TestMigratedCleanupUsesOwnedBridgesAfterTopologyChanges(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"rename", "remove", "pings", "p2p"} {
		for _, recreate := range []bool{false, true} {
			t.Run(change+"/recreate="+strconv.FormatBool(recreate), func(t *testing.T) {
				path, topology, _, configPath := legacyStateFixture(t)
				manager := NewManager(nil, nil)
				if err := manager.MigrateState(context.Background(), path); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				text := string(data)
				switch change {
				case "rename":
					text = strings.Replace(text, "name: lan", "name: renamed", 1)
				case "remove":
					text = strings.Split(text, "links:")[0]
				case "p2p":
					text = strings.Replace(text, "type: bridge", "type: p2p", 1)
				case "pings":
					text += "pings:\n  - name: added\n    from: {router: r1}\n    to: 192.0.2.1\n"
				}
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
				changed, err := config.LoadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				fakeDocker, fakeNetwork := &fakeDockerClient{}, &fakeNetworkManager{}
				manager = NewManager(fakeDocker, fakeNetwork)
				if recreate {
					err = manager.up(context.Background(), path, UpOptions{Recreate: true})
				} else {
					err = manager.cleanupRuntime(context.Background(), changed, nil, false)
				}
				if err != nil {
					t.Fatal(err)
				}
				if !hasDeletedLink(fakeNetwork, bridgeLinkName(topology.Lab.Name, "lan")) || hasDeletedLink(fakeNetwork, bridgeLinkName(topology.Lab.Name, "renamed")) || hasDeletedLink(fakeNetwork, bridgeLinkName("another-lab", "lan")) {
					t.Fatalf("cleanup did not use only owned bridges: %v", fakeNetwork.ops)
				}
				if !slices.Contains(fakeDocker.removed, "original-r1") || !slices.Contains(fakeDocker.removed, "original-r2") {
					t.Fatalf("original container IDs not cleaned: %v", fakeDocker.removed)
				}
				if data, err := os.ReadFile(configPath); err != nil || string(data) != "preserve original\n" {
					t.Fatalf("configuration not retained: %q %v", data, err)
				}
				if recreate {
					snapshot, err := state.Read(changed.StatePath())
					if err != nil || snapshot.OwnedBridges == nil {
						t.Fatalf("new inventory missing: %v %v", snapshot, err)
					}
					want := map[string]string{}
					if change == "rename" {
						want["renamed"] = bridgeLinkName(topology.Lab.Name, "renamed")
					} else if change == "pings" {
						want["lan"] = bridgeLinkName(topology.Lab.Name, "lan")
					}
					if !reflect.DeepEqual(snapshot.OwnedBridges, want) {
						t.Fatalf("new inventory=%v want=%v", snapshot.OwnedBridges, want)
					}
					if err := manager.cleanupRuntime(context.Background(), changed, nil, false); err != nil {
						t.Fatalf("modern empty/nonempty inventory cleanup: %v", err)
					}
				}
			})
		}
	}
}

func TestCleanupAndLookupRejectUnsafeStateBeforeSideEffects(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"legacy", "malformed", "foreign-lab", "foreign-bridge", "missing-containers", "empty-id"} {
		t.Run(failure, func(t *testing.T) {
			path, topology, snapshot, configPath := legacyStateFixture(t)
			if failure != "legacy" {
				snapshot.OwnedBridges = map[string]string{"lan": bridgeLinkName(topology.Lab.Name, "lan")}
			}
			switch failure {
			case "foreign-lab":
				snapshot.Lab = "another-lab"
			case "foreign-bridge":
				snapshot.OwnedBridges["lan"] = "unowned0"
			case "missing-containers":
				snapshot.Containers = nil
			case "empty-id":
				snapshot.Containers["r1"] = state.ContainerState{}
			}
			if err := state.Write(topology.StatePath(), snapshot); err != nil {
				t.Fatal(err)
			}
			if failure == "malformed" {
				if err := os.WriteFile(topology.StatePath(), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(topology.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			manager := NewManager(nil, nil) // Any Docker/network call is a test failure.
			if err := manager.cleanupRuntime(context.Background(), topology, nil, true); err == nil {
				t.Fatal("unsafe cleanup accepted")
			}
			if err := manager.up(context.Background(), path, UpOptions{Recreate: true, Reseed: true}); err == nil {
				t.Fatal("unsafe recreate accepted")
			}
			if failure != "legacy" {
				if _, err := manager.lookupRouterContainer(context.Background(), topology, "r1"); err == nil {
					t.Fatal("unsafe container lookup accepted")
				}
			}
			if after, err := os.ReadFile(topology.StatePath()); err != nil || !slices.Equal(after, before) {
				t.Fatal("rejected operation changed state")
			}
			if data, err := os.ReadFile(configPath); err != nil || string(data) != "preserve original\n" {
				t.Fatal("rejected operation changed config")
			}
		})
	}
}

func TestPartialBridgeCreationFailedRollbackRetainsInventoryForChangedYAMLRetry(t *testing.T) {
	t.Parallel()
	path := startupTopologyFile(t)
	creationErr, cleanupErr := errors.New("partial bridge setup failure"), errors.New("bridge deletion failure")
	fakeDocker := &fakeDockerClient{}
	fakeNetwork := &fakeNetworkManager{ensureFn: func(string, int) error { return creationErr }, deleteErr: cleanupErr}
	manager := NewManager(fakeDocker, fakeNetwork)
	if err := manager.up(context.Background(), path, UpOptions{}); !errors.Is(err, creationErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("startup/rollback errors not preserved: %v", err)
	}
	topology, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.Read(topology.StatePath())
	if err != nil || !reflect.DeepEqual(snapshot.OwnedBridges, map[string]string{"lan": bridgeLinkName("startup", "lan")}) || len(snapshot.Containers) != 2 {
		t.Fatalf("failed rollback inventory=%v err=%v", snapshot, err)
	}
	topology.Links = nil
	fakeNetwork.ops, fakeNetwork.deleteErr = nil, nil
	if err := manager.cleanupRuntime(context.Background(), topology, nil, false); err != nil {
		t.Fatal(err)
	}
	if !hasDeletedLink(fakeNetwork, bridgeLinkName("startup", "lan")) {
		t.Fatal("retry lost partially created bridge after YAML changed")
	}
	if _, err := os.Stat(topology.StatePath()); !os.IsNotExist(err) {
		t.Fatal("successful retry retained state")
	}
}

func hasDeletedLink(network *fakeNetworkManager, name string) bool {
	return slices.ContainsFunc(network.ops, func(op []string) bool { return len(op) == 2 && op[0] == "delete-link" && op[1] == name })
}
