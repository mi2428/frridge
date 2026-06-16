package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
    command: ["/bin/sh", "-lc", "sleep infinity"]
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
	if got, want := strings.Join(router.Command, " "), "/bin/sh -lc sleep infinity"; got != want {
		t.Fatalf("ResolveRouter().Command = %q, want %q", got, want)
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

func TestLoadFileAppliesBuiltInRouterDefaults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	topologyPath := filepath.Join(tmpDir, "lab.yaml")
	if err := os.WriteFile(topologyPath, []byte(`
apiVersion: frridge/v1alpha1
lab:
  name: builtins
routers:
  r1: {}
  r2: {}
links:
  - name: uplink
    type: p2p
    members:
      - router: r1
        ifname: eth1
      - router: r2
        ifname: eth1
`), 0o644); err != nil {
		t.Fatalf("write topology: %v", err)
	}

	topology, err := LoadFile(topologyPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	router := topology.ResolveRouter("r1")
	if got, want := router.Image, DefaultRouterImage; got != want {
		t.Fatalf("ResolveRouter().Image = %q, want %q", got, want)
	}
	if got, want := router.Privileged, DefaultRouterPrivileged; got != want {
		t.Fatalf("ResolveRouter().Privileged = %t, want %t", got, want)
	}
	if got := router.Sysctls["net.ipv4.ip_forward"]; got != "1" {
		t.Fatalf("ResolveRouter().Sysctls[ip_forward] = %q, want 1", got)
	}
	if got := router.Sysctls["net.ipv4.conf.all.rp_filter"]; got != "0" {
		t.Fatalf("ResolveRouter().Sysctls[rp_filter] = %q, want 0", got)
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

func TestValidateAcceptsNamedPingChecks(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "ping-lab", Defaults: Defaults{Image: "frr"}},
		Routers: map[string]Router{
			"r1": {},
			"r2": {},
		},
		Links: []Link{
			{
				Name: "fabric",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth1"},
				},
			},
		},
		Pings: []Ping{
			{
				Name: "r1-to-r2",
				From: PingSource{
					Router:    "r1",
					Namespace: "host",
					Address:   "10.10.10.11",
				},
				To:    "192.0.2.2",
				Count: 3,
			},
		},
	}

	if err := topology.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsEmptyCommandToken(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "bad-command"},
		Routers: map[string]Router{
			"r1": {Command: []string{"sleep", ""}},
			"r2": {},
		},
		Links: []Link{
			{
				Name: "fabric",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth1"},
				},
			},
		},
	}

	err := topology.Validate()
	if err == nil || !strings.Contains(err.Error(), `router "r1" has an empty command token`) {
		t.Fatalf("Validate() error = %v, want empty command token error", err)
	}
}

func TestValidateAcceptsLinuxDataplaneConfig(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "linux-lab"},
		Routers: map[string]Router{
			"r1": {
				Linux: Linux{
					Routes: []Route{
						{
							To:  "10.255.0.2/32",
							Via: "192.0.2.1",
						},
					},
					Bridges: []Bridge{
						{
							Name:       "br10",
							Addresses:  []string{"10.10.10.1/24"},
							Interfaces: []string{"eth1"},
							VXLANS: []VXLAN{
								{
									Name:       "vxlan100",
									VNI:        100,
									Local:      "10.255.0.1",
									NoLearning: true,
								},
							},
							Namespaces: []Namespace{
								{
									Name:       "host",
									IfName:     "eth0",
									MAC:        "02:00:00:00:10:11",
									Addresses:  []string{"10.10.10.11/24"},
									DefaultVia: "10.10.10.1",
								},
							},
						},
					},
				},
			},
			"r2": {},
		},
		Links: []Link{
			{
				Name: "fabric",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth1"},
				},
			},
		},
	}

	if err := topology.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsLinuxRouteWithoutViaOrDevice(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "linux-lab"},
		Routers: map[string]Router{
			"r1": {
				Linux: Linux{
					Routes: []Route{
						{To: "10.255.0.2/32"},
					},
				},
			},
			"r2": {},
		},
		Links: []Link{
			{
				Name: "fabric",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth1"},
				},
			},
		},
	}

	err := topology.Validate()
	if err == nil || !strings.Contains(err.Error(), `must set via, dev, or both`) {
		t.Fatalf("Validate() error = %v, want linux route validation error", err)
	}
}

func TestValidateRejectsPingWithoutSourceRouter(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "ping-lab", Defaults: Defaults{Image: "frr"}},
		Routers: map[string]Router{
			"r1": {},
			"r2": {},
		},
		Links: []Link{
			{
				Name: "fabric",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth1"},
				},
			},
		},
		Pings: []Ping{
			{
				Name: "bad",
				From: PingSource{Router: "missing"},
				To:   "192.0.2.2",
			},
		},
	}

	err := topology.Validate()
	if err == nil || !strings.Contains(err.Error(), `undefined source router "missing"`) {
		t.Fatalf("Validate() error = %v, want undefined source router error", err)
	}
}

func TestValidateRejectsPingWithInvalidSourceAddress(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "ping-lab", Defaults: Defaults{Image: "frr"}},
		Routers: map[string]Router{
			"r1": {},
			"r2": {},
		},
		Links: []Link{
			{
				Name: "fabric",
				Type: "p2p",
				Members: []LinkMember{
					{Router: "r1", IfName: "eth1"},
					{Router: "r2", IfName: "eth1"},
				},
			},
		},
		Pings: []Ping{
			{
				Name: "bad",
				From: PingSource{
					Router:  "r1",
					Address: "not-an-ip",
				},
				To: "192.0.2.2",
			},
		},
	}

	err := topology.Validate()
	if err == nil || !strings.Contains(err.Error(), `invalid source address`) {
		t.Fatalf("Validate() error = %v, want invalid source address error", err)
	}
}

func TestExampleLabsLoad(t *testing.T) {
	t.Parallel()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	examplesDir := filepath.Join(root, "examples")

	var labs []string
	err = filepath.WalkDir(examplesDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "lab.yaml" {
			labs = append(labs, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%q) error = %v", examplesDir, err)
	}
	if len(labs) == 0 {
		t.Fatalf("no example labs found below %q", examplesDir)
	}

	slices.Sort(labs)
	for _, path := range labs {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel(%q) error = %v", path, err)
		}

		t.Run(rel, func(t *testing.T) {
			t.Parallel()

			topology, err := LoadFile(path)
			if err != nil {
				t.Fatalf("LoadFile(%q) error = %v", path, err)
			}
			if _, err := topology.Digest(); err != nil {
				t.Fatalf("Digest(%q) error = %v", path, err)
			}
		})
	}
}
