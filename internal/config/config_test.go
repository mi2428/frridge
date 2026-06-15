package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileAppliesDefaultsAndResolvesPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	topologyPath := filepath.Join(tmpDir, "lab.yaml")
	if err := os.WriteFile(topologyPath, []byte(`
apiVersion: frridge/v1alpha1
lab:
  name: test-lab
  defaults:
    image: frrouting/frr:latest
    sysctls:
      net.ipv4.ip_forward: "1"
routers:
  r1:
    mounts:
      - source: ./artifacts
        target: /lab
    sysctls:
      net.ipv4.conf.all.rp_filter: "0"
links:
  - name: uplink
    type: p2p
    members:
      - router: r1
        ifname: eth1
      - router: r1
        ifname: eth2
`), 0o644); err != nil {
		t.Fatalf("write topology: %v", err)
	}

	topology, err := LoadFile(topologyPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	router := topology.ResolveRouter("r1")
	if router.Image != "frrouting/frr:latest" {
		t.Fatalf("ResolveRouter().Image = %q, want default image", router.Image)
	}
	if got, want := router.Hostname, "r1"; got != want {
		t.Fatalf("ResolveRouter().Hostname = %q, want %q", got, want)
	}
	if got, want := router.Mounts[0].Source, filepath.Join(tmpDir, "artifacts"); got != want {
		t.Fatalf("ResolveRouter().Mounts[0].Source = %q, want %q", got, want)
	}
	if got := router.Sysctls["net.ipv4.ip_forward"]; got != "1" {
		t.Fatalf("default sysctl missing, got %q", got)
	}
	if got := router.Sysctls["net.ipv4.conf.all.rp_filter"]; got != "0" {
		t.Fatalf("router sysctl missing, got %q", got)
	}
	if got, want := topology.WorkDirPath(), filepath.Join(tmpDir, DefaultWorkDir); got != want {
		t.Fatalf("WorkDirPath() = %q, want %q", got, want)
	}
}

func TestValidateRejectsDuplicateInterfacesAcrossLinks(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "dup-if", Defaults: Defaults{Image: "frr"}},
		Routers: map[string]Router{
			"r1": {},
			"r2": {},
		},
		Links: []Link{
			{
				Name: "l1",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth1"},
				},
			},
			{
				Name: "l2",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth2"},
				},
			},
		},
	}

	err := topology.Validate()
	if err == nil || !strings.Contains(err.Error(), `router "r1" reuses interface "eth1"`) {
		t.Fatalf("Validate() error = %v, want duplicate interface error", err)
	}
}

func TestDigestStableAcrossLoads(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	topologyPath := filepath.Join(tmpDir, "lab.yaml")
	data := []byte(`
apiVersion: frridge/v1alpha1
lab:
  name: digest-lab
  defaults:
    image: frrouting/frr:latest
routers:
  a: {}
  b: {}
links:
  - name: fabric
    type: p2p
    members:
      - { router: a, ifname: eth1 }
      - { router: b, ifname: eth1 }
`)
	if err := os.WriteFile(topologyPath, data, 0o644); err != nil {
		t.Fatalf("write topology: %v", err)
	}

	first, err := LoadFile(topologyPath)
	if err != nil {
		t.Fatalf("LoadFile(first) error = %v", err)
	}
	second, err := LoadFile(topologyPath)
	if err != nil {
		t.Fatalf("LoadFile(second) error = %v", err)
	}

	digest1, err := first.Digest()
	if err != nil {
		t.Fatalf("first.Digest() error = %v", err)
	}
	digest2, err := second.Digest()
	if err != nil {
		t.Fatalf("second.Digest() error = %v", err)
	}
	if digest1 != digest2 {
		t.Fatalf("Digest mismatch: %q != %q", digest1, digest2)
	}
}

func TestResolveWorkDirHonorsOverrideEnv(t *testing.T) {
	tmpDir := t.TempDir()
	override := filepath.Join(tmpDir, "guest-state")
	t.Setenv(WorkDirOverrideEnv, override)

	if got, want := ResolveWorkDir(filepath.Join(tmpDir, "lab"), "ignored"), override; got != want {
		t.Fatalf("ResolveWorkDir() = %q, want %q", got, want)
	}
}
