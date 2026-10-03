package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadFileValidatesStatePathNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", " ", ".", "..", "../escape", "nested/name", "/absolute", "trailing/", "nul\x00name", "lab-1_r.2", "with spaces", "名前", `a\b`, "..safe", "a:b"} {
		valid := name == "lab-1_r.2" || name == "with spaces" || name == "名前" || name == `a\b` || name == "..safe" || name == "a:b"
		for _, field := range []string{"lab", "router"} {
			t.Run(fmt.Sprintf("%s/%q", field, name), func(t *testing.T) {
				t.Parallel()
				labName, routerName := "safe-lab", "r1"
				if field == "lab" {
					labName = name
				} else {
					routerName = name
				}
				path := filepath.Join(t.TempDir(), "lab.yaml")
				data := fmt.Sprintf("apiVersion: frridge/v1alpha1\nlab:\n  name: %q\nrouters:\n  %q: {}\n", labName, routerName)
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				topology, err := LoadFile(path)
				if !valid {
					if err == nil {
						t.Fatal("LoadFile() error = nil, want invalid state path name")
					}
					return
				}
				if err != nil {
					t.Fatalf("LoadFile() error = %v", err)
				}
				if got, want := topology.LabDir(), filepath.Join(topology.WorkDirPath(), "labs", labName); got != want {
					t.Fatalf("LabDir() = %q, want %q", got, want)
				}
				if got, want := topology.RouterDir(routerName), filepath.Join(topology.LabDir(), "routers", routerName); got != want {
					t.Fatalf("RouterDir() = %q, want %q", got, want)
				}
			})
		}
	}
}

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

func TestLoadFileValidatesLinuxDeviceCollisions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		linux     Linux
		linkName  string
		wantError string
	}{
		{name: "link vs vrf", linux: Linux{VRFs: []VRF{{Name: "eth1", Table: 100}}}, wantError: `device name "eth1"`},
		{name: "link vs bond", linux: Linux{Bonds: []Bond{{Name: "eth1", Mode: "active-backup"}}}, wantError: `device name "eth1"`},
		{name: "link vs bridge", linux: Linux{Bridges: []Bridge{{Name: "eth1"}}}, wantError: `device name "eth1"`},
		{name: "link vs vxlan", linux: Linux{Bridges: []Bridge{{Name: "br0", VXLANS: []VXLAN{{Name: "eth1", VNI: 100}}}}}, wantError: `device name "eth1"`},
		{name: "link vs veth", linux: Linux{Veths: []Veth{{Name: "eth1", Peer: "host0"}}}, wantError: `device name "eth1"`},
		{name: "trimmed link vs veth", linkName: " eth1 ", linux: Linux{Veths: []Veth{{Name: "eth1", Peer: "host0"}}}, wantError: `device name "eth1"`},
		{name: "link vs peer", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "eth1"}}}, wantError: `device name "eth1"`},
		{name: "link vs moved peer", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "eth1", Namespace: &Namespace{Name: "host", IfName: "eth0"}}}}, wantError: `device name "eth1"`},
		{name: "bridge vs peer", linux: Linux{Bridges: []Bridge{{Name: "host0"}}, Veths: []Veth{{Name: "lan0", Peer: "host0"}}}, wantError: `device name "host0"`},
		{name: "peer vs later device", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "host0"}, {Name: "host0", Peer: "host1"}}}, wantError: `device name "host0"`},
		{name: "device vs later peer", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "host0"}, {Name: "lan1", Peer: "lan0"}}}, wantError: `device name "lan0"`},
		{name: "loopback link", linkName: "lo", wantError: "loopback"},
		{name: "trimmed loopback link", linkName: " lo ", wantError: "loopback"},
		{name: "loopback device", linux: Linux{Veths: []Veth{{Name: "lo", Peer: "host0"}}}, wantError: `device name "lo"`},
		{name: "loopback peer", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "lo"}}}, wantError: `device name "lo"`},
		{name: "namespace loopback", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "host0", Namespace: &Namespace{Name: "host", IfName: "lo"}}}}, wantError: "loopback"},
		{name: "configure existing interfaces", linux: Linux{Interfaces: []Interface{{Name: "eth1"}, {Name: "lo"}}}},
		{name: "configure trimmed link", linkName: " eth1 ", linux: Linux{Interfaces: []Interface{{Name: "eth1"}}}},
		{name: "bond attachment", linux: Linux{Bonds: []Bond{{Name: "bond0", Mode: "active-backup", Interfaces: []string{"eth1"}}}}},
		{name: "bridge attachment", linux: Linux{Bridges: []Bridge{{Name: "br0", Interfaces: []string{"eth1"}}}}},
		{name: "same name in another namespace", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "host0", Namespace: &Namespace{Name: "host", IfName: "eth1"}}}}},
		{name: "reuse moved peer name", linux: Linux{Veths: []Veth{{Name: "lan0", Peer: "host0", Namespace: &Namespace{Name: "host", IfName: "eth0"}}, {Name: "host0", Peer: "host1"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ifName := tc.linkName
			if ifName == "" {
				ifName = "eth1"
			}
			topology := Topology{
				APIVersion: APIVersion,
				Lab:        Lab{Name: "device-collisions"},
				Routers: map[string]Router{
					"r1": {Linux: tc.linux},
					"r2": {Linux: Linux{VRFs: []VRF{{Name: "eth1", Table: 100}}}},
				},
				Links: []Link{{Name: "l1", Type: "p2p", Members: []LinkMember{
					{Router: "r1", IfName: ifName}, {Router: "r2", IfName: "eth2"},
				}}},
			}
			data, err := yaml.Marshal(topology)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			path := filepath.Join(t.TempDir(), "lab.yaml")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			_, err = LoadFile(path)
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("LoadFile() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("LoadFile() error = %v, want %q", err, tc.wantError)
			}
		})
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

	learningOff := false
	neighOn := true
	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "linux-lab"},
		Routers: map[string]Router{
			"r1": {
				Linux: Linux{
					VRFs: []VRF{
						{Name: "tenant", Table: 1100},
					},
					Bonds: []Bond{
						{
							Name:       "bond0",
							Mode:       "active-backup",
							Master:     "br10",
							Interfaces: []string{"eth2"},
						},
					},
					Interfaces: []Interface{
						{
							Name:      "eth3",
							Master:    "tenant",
							Addresses: []string{"10.20.10.1/24"},
						},
					},
					Veths: []Veth{
						{
							Name:      "lan0",
							Peer:      "host0",
							Master:    "tenant",
							Addresses: []string{"10.10.10.1/24"},
							Namespace: &Namespace{
								Name:       "host",
								IfName:     "eth0",
								MAC:        "02:00:00:00:10:11",
								Addresses:  []string{"10.10.10.11/24"},
								DefaultVia: "10.10.10.1",
							},
						},
					},
					Routes: []Route{
						{
							To:  "10.255.0.2/32",
							Via: "192.0.2.1",
						},
					},
					Bridges: []Bridge{
						{
							Name:        "br10",
							Master:      "tenant",
							MAC:         "02:00:00:00:10:01",
							AddrGenMode: "none",
							Interfaces:  []string{"eth1"},
							VXLANS: []VXLAN{
								{
									Name:        "vxlan100",
									VNI:         100,
									Local:       "10.255.0.1",
									NoLearning:  true,
									AddrGenMode: "none",
									BridgeSlave: BridgeSlaveOptions{
										Learning:      &learningOff,
										NeighSuppress: &neighOn,
									},
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

func TestValidateRejectsDuplicateLinuxDataplaneInterfaceAttachment(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "linux-lab"},
		Routers: map[string]Router{
			"r1": {
				Linux: Linux{
					Bonds: []Bond{
						{
							Name:       "bond0",
							Mode:       "active-backup",
							Interfaces: []string{"eth1"},
						},
					},
					Bridges: []Bridge{
						{
							Name:       "br10",
							Interfaces: []string{"eth1"},
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

	err := topology.Validate()
	if err == nil || !strings.Contains(err.Error(), `assigns conflicting linux masters to link "eth1"`) {
		t.Fatalf("Validate() error = %v, want conflicting linux master assignment", err)
	}
}

func TestValidateRejectsConflictingLinuxMasterAssignments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		linux  Linux
		target string
	}{
		{
			name: "bridge slave also configured with another master",
			linux: Linux{
				Bridges: []Bridge{
					{
						Name:       "br10",
						Interfaces: []string{"eth1"},
					},
				},
				Interfaces: []Interface{
					{
						Name:   "eth1",
						Master: "tenant",
					},
				},
			},
			target: `link "eth1"`,
		},
		{
			name: "vxlan auto-bridge conflicts with explicit interface master",
			linux: Linux{
				Bridges: []Bridge{
					{
						Name: "br10",
						VXLANS: []VXLAN{
							{
								Name: "vxlan100",
								VNI:  100,
							},
						},
					},
				},
				Interfaces: []Interface{
					{
						Name:   "vxlan100",
						Master: "tenant",
					},
				},
			},
			target: `link "vxlan100"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			topology := &Topology{
				APIVersion: APIVersion,
				Lab:        Lab{Name: "linux-lab"},
				Routers: map[string]Router{
					"r1": {Linux: tc.linux},
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
			if err == nil || !strings.Contains(err.Error(), tc.target) || !strings.Contains(err.Error(), "assigns conflicting linux masters") {
				t.Fatalf("Validate() error = %v, want conflicting linux master assignment for %s", err, tc.target)
			}
		})
	}
}

func TestValidateRejectsInvalidLinuxAddrGenMode(t *testing.T) {
	t.Parallel()

	topology := &Topology{
		APIVersion: APIVersion,
		Lab:        Lab{Name: "linux-lab"},
		Routers: map[string]Router{
			"r1": {
				Linux: Linux{
					Bridges: []Bridge{
						{
							Name:        "br10",
							AddrGenMode: "bad-mode",
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

	err := topology.Validate()
	if err == nil || !strings.Contains(err.Error(), `invalid addrgenmode`) {
		t.Fatalf("Validate() error = %v, want invalid addrgenmode error", err)
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
