// Package config loads, validates, and resolves lab topology YAML files.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// APIVersion is the only topology schema version currently understood by
	// frridge.
	APIVersion = "frridge/v1alpha1"
	// DefaultWorkDir is used when lab.workdir is omitted in the topology.
	DefaultWorkDir = ".frridge"
	// DefaultRouterImage is used when both router.image and lab.defaults.image
	// are omitted.
	DefaultRouterImage = "frridge-frr:latest"
	// DefaultRouterPrivileged is used when privileged is omitted everywhere.
	DefaultRouterPrivileged = true
	// WorkDirOverrideEnv lets wrappers such as frridge-mp move generated lab
	// state onto a guest-local filesystem without rewriting the topology file.
	WorkDirOverrideEnv = "FRRIDGE_WORKDIR_OVERRIDE"
)

var sysctlKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Topology is the fully decoded lab definition together with source-path
// metadata used for resolving relative paths on disk.
type Topology struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Lab        Lab               `yaml:"lab" json:"lab"`
	Routers    map[string]Router `yaml:"routers" json:"routers"`
	Links      []Link            `yaml:"links" json:"links"`
	Pings      []Ping            `yaml:"pings" json:"pings,omitempty"`

	sourcePath string
	baseDir    string
}

// Lab holds lab-wide settings that apply across all routers.
type Lab struct {
	Name     string   `yaml:"name" json:"name"`
	WorkDir  string   `yaml:"workdir" json:"workdir"`
	Defaults Defaults `yaml:"defaults" json:"defaults"`
}

// Defaults defines router attributes inherited when a router omits them.
type Defaults struct {
	Image      string            `yaml:"image" json:"image"`
	Privileged *bool             `yaml:"privileged" json:"privileged,omitempty"`
	Sysctls    map[string]string `yaml:"sysctls" json:"sysctls,omitempty"`
}

// Router describes one FRR container in the topology file.
type Router struct {
	Hostname   string            `yaml:"hostname" json:"hostname"`
	Image      string            `yaml:"image" json:"image"`
	Privileged *bool             `yaml:"privileged" json:"privileged,omitempty"`
	Env        map[string]string `yaml:"env" json:"env,omitempty"`
	Loopbacks  []string          `yaml:"loopbacks" json:"loopbacks,omitempty"`
	Mounts     []Mount           `yaml:"mounts" json:"mounts,omitempty"`
	Sysctls    map[string]string `yaml:"sysctls" json:"sysctls,omitempty"`
	Linux      Linux             `yaml:"linux" json:"linux"`
	Commands   []Command         `yaml:"commands" json:"commands,omitempty"`
}

// Mount describes one bind mount injected into a router container.
type Mount struct {
	Source   string `yaml:"source" json:"source"`
	Target   string `yaml:"target" json:"target"`
	ReadOnly bool   `yaml:"readOnly" json:"readOnly,omitempty"`
}

// Command is one startup action executed in a router after links exist.
type Command struct {
	Kind string `yaml:"kind" json:"kind"`
	Run  string `yaml:"run" json:"run"`
}

// Linux describes router-local Linux dataplane objects that frridge can build
// automatically after interfaces and loopbacks exist.
type Linux struct {
	Routes  []Route  `yaml:"routes" json:"routes,omitempty"`
	Bridges []Bridge `yaml:"bridges" json:"bridges,omitempty"`
}

// Route installs one static route in the router's default namespace.
type Route struct {
	To  string `yaml:"to" json:"to"`
	Via string `yaml:"via" json:"via,omitempty"`
	Dev string `yaml:"dev" json:"dev,omitempty"`
}

// Bridge describes one bridge device and its attached Linux endpoints.
type Bridge struct {
	Name       string      `yaml:"name" json:"name"`
	Addresses  []string    `yaml:"addresses" json:"addresses,omitempty"`
	Interfaces []string    `yaml:"interfaces" json:"interfaces,omitempty"`
	VXLANS     []VXLAN     `yaml:"vxlans" json:"vxlans,omitempty"`
	Namespaces []Namespace `yaml:"namespaces" json:"namespaces,omitempty"`
}

// VXLAN describes one VXLAN device enslaved to a bridge.
type VXLAN struct {
	Name       string `yaml:"name" json:"name"`
	VNI        int    `yaml:"vni" json:"vni"`
	Local      string `yaml:"local" json:"local,omitempty"`
	DstPort    int    `yaml:"dstport" json:"dstport,omitempty"`
	NoLearning bool   `yaml:"nolearning" json:"nolearning,omitempty"`
}

// Namespace describes one veth-backed Linux namespace attached to a bridge.
type Namespace struct {
	Name       string   `yaml:"name" json:"name"`
	IfName     string   `yaml:"ifname" json:"ifname"`
	MAC        string   `yaml:"mac" json:"mac,omitempty"`
	Addresses  []string `yaml:"addresses" json:"addresses,omitempty"`
	DefaultVia string   `yaml:"defaultVia" json:"defaultVia,omitempty"`
}

// Link models either a shared bridge segment or a point-to-point veth pair.
type Link struct {
	Name    string       `yaml:"name" json:"name"`
	Type    string       `yaml:"type" json:"type"`
	MTU     int          `yaml:"mtu" json:"mtu,omitempty"`
	Members []LinkMember `yaml:"members" json:"members"`
}

// LinkMember describes one router interface participating in a Link.
type LinkMember struct {
	Router string `yaml:"router" json:"router"`
	IfName string `yaml:"ifname" json:"ifname"`
	IPv4   string `yaml:"ipv4" json:"ipv4,omitempty"`
	MAC    string `yaml:"mac" json:"mac,omitempty"`
}

// Ping describes one connectivity check that the `frridge ping` command can
// execute after a lab is up.
type Ping struct {
	Name  string     `yaml:"name" json:"name"`
	From  PingSource `yaml:"from" json:"from"`
	To    string     `yaml:"to" json:"to"`
	Count int        `yaml:"count" json:"count,omitempty"`
}

// PingSource describes where a Ping check should be executed from.
type PingSource struct {
	Router    string `yaml:"router" json:"router"`
	Namespace string `yaml:"namespace" json:"namespace,omitempty"`
	Address   string `yaml:"address" json:"address,omitempty"`
}

// ResolvedRouter is a Router after lab defaults and relative paths have been
// merged into concrete runtime values.
type ResolvedRouter struct {
	Name       string
	Hostname   string
	Image      string
	Privileged bool
	Env        map[string]string
	Loopbacks  []string
	Mounts     []Mount
	Sysctls    map[string]string
	Linux      Linux
	Commands   []Command
}

// LoadFile reads a topology file, validates it, and records the source
// location needed for later path resolution.
func LoadFile(path string) (*Topology, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve topology path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read topology file: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var topology Topology
	if err := decoder.Decode(&topology); err != nil {
		return nil, fmt.Errorf("decode topology: %w", err)
	}

	topology.sourcePath = absPath
	topology.baseDir = filepath.Dir(absPath)
	if err := topology.Validate(); err != nil {
		return nil, err
	}

	return &topology, nil
}

// Validate checks schema constraints that frridge relies on before touching
// Docker or host networking state.
func (t *Topology) Validate() error {
	if t.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", t.APIVersion)
	}
	if strings.TrimSpace(t.Lab.Name) == "" {
		return fmt.Errorf("lab.name is required")
	}
	if len(t.Routers) == 0 {
		return fmt.Errorf("at least one router is required")
	}

	linkNames := make(map[string]struct{}, len(t.Links))
	ifNames := make(map[string]map[string]string, len(t.Routers))
	pingNames := make(map[string]struct{}, len(t.Pings))

	for name, router := range t.Routers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("router name must not be empty")
		}

		if _, ok := ifNames[name]; !ok {
			ifNames[name] = make(map[string]string)
		}

		resolved := t.ResolveRouter(name)
		if resolved.Image == "" {
			return fmt.Errorf("router %q resolved to an empty image", name)
		}

		for _, loopback := range router.Loopbacks {
			if _, _, err := net.ParseCIDR(loopback); err != nil {
				return fmt.Errorf("router %q has invalid loopback %q: %w", name, loopback, err)
			}
		}
		for _, mount := range router.Mounts {
			if strings.TrimSpace(mount.Source) == "" {
				return fmt.Errorf("router %q has a mount with empty source", name)
			}
			if strings.TrimSpace(mount.Target) == "" {
				return fmt.Errorf("router %q has a mount with empty target", name)
			}
			if !filepath.IsAbs(mount.Target) {
				return fmt.Errorf("router %q mount target %q must be absolute", name, mount.Target)
			}
		}
		for key := range mergedSysctls(t.Lab.Defaults.Sysctls, router.Sysctls) {
			if !sysctlKeyPattern.MatchString(key) {
				return fmt.Errorf("router %q has invalid sysctl key %q", name, key)
			}
		}
		if err := validateLinux(name, router.Linux); err != nil {
			return err
		}
		for _, command := range router.Commands {
			switch command.Kind {
			case "shell", "vtysh":
			default:
				return fmt.Errorf("router %q has unsupported command kind %q", name, command.Kind)
			}
			if strings.TrimSpace(command.Run) == "" {
				return fmt.Errorf("router %q has an empty %s command", name, command.Kind)
			}
		}
	}

	for _, link := range t.Links {
		if strings.TrimSpace(link.Name) == "" {
			return fmt.Errorf("link name is required")
		}
		if _, ok := linkNames[link.Name]; ok {
			return fmt.Errorf("duplicate link name %q", link.Name)
		}
		linkNames[link.Name] = struct{}{}

		switch link.Type {
		case "bridge", "p2p":
		default:
			return fmt.Errorf("link %q has unsupported type %q", link.Name, link.Type)
		}
		if link.MTU < 0 {
			return fmt.Errorf("link %q has invalid mtu %d", link.Name, link.MTU)
		}
		if link.Type == "bridge" && len(link.Members) < 2 {
			return fmt.Errorf("bridge link %q must have at least two members", link.Name)
		}
		if link.Type == "p2p" && len(link.Members) != 2 {
			return fmt.Errorf("p2p link %q must have exactly two members", link.Name)
		}

		for _, member := range link.Members {
			if _, ok := t.Routers[member.Router]; !ok {
				return fmt.Errorf("link %q references undefined router %q", link.Name, member.Router)
			}
			if strings.TrimSpace(member.IfName) == "" {
				return fmt.Errorf("link %q has a member with empty ifname", link.Name)
			}
			if owner, ok := ifNames[member.Router][member.IfName]; ok {
				return fmt.Errorf("router %q reuses interface %q across links %q and %q", member.Router, member.IfName, owner, link.Name)
			}
			ifNames[member.Router][member.IfName] = link.Name

			if member.IPv4 != "" {
				if _, _, err := net.ParseCIDR(member.IPv4); err != nil {
					return fmt.Errorf("link %q member %s/%s has invalid ipv4 %q: %w", link.Name, member.Router, member.IfName, member.IPv4, err)
				}
			}
			if member.MAC != "" {
				if _, err := net.ParseMAC(member.MAC); err != nil {
					return fmt.Errorf("link %q member %s/%s has invalid mac %q: %w", link.Name, member.Router, member.IfName, member.MAC, err)
				}
			}
		}
	}

	for _, ping := range t.Pings {
		if strings.TrimSpace(ping.Name) == "" {
			return fmt.Errorf("ping name is required")
		}
		if _, ok := pingNames[ping.Name]; ok {
			return fmt.Errorf("duplicate ping name %q", ping.Name)
		}
		pingNames[ping.Name] = struct{}{}

		if _, ok := t.Routers[ping.From.Router]; !ok {
			return fmt.Errorf("ping %q references undefined source router %q", ping.Name, ping.From.Router)
		}
		if strings.TrimSpace(ping.From.Address) != "" && net.ParseIP(strings.TrimSpace(ping.From.Address)) == nil {
			return fmt.Errorf("ping %q has invalid source address %q", ping.Name, ping.From.Address)
		}
		if strings.TrimSpace(ping.To) == "" {
			return fmt.Errorf("ping %q target must not be empty", ping.Name)
		}
		if ping.Count < 0 {
			return fmt.Errorf("ping %q has invalid count %d", ping.Name, ping.Count)
		}
	}

	return nil
}

// ResolveRouter merges lab defaults into one router and resolves relative
// mount sources against the topology file location.
func (t *Topology) ResolveRouter(name string) ResolvedRouter {
	router := t.Routers[name]
	hostname := strings.TrimSpace(router.Hostname)
	if hostname == "" {
		hostname = name
	}

	image := strings.TrimSpace(router.Image)
	if image == "" {
		image = strings.TrimSpace(t.Lab.Defaults.Image)
	}
	if image == "" {
		image = DefaultRouterImage
	}

	privileged := DefaultRouterPrivileged
	if t.Lab.Defaults.Privileged != nil {
		privileged = *t.Lab.Defaults.Privileged
	}
	if router.Privileged != nil {
		privileged = *router.Privileged
	}

	mounts := make([]Mount, 0, len(router.Mounts))
	for _, mount := range router.Mounts {
		mounts = append(mounts, Mount{
			Source:   t.ResolvePath(mount.Source),
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
	}

	return ResolvedRouter{
		Name:       name,
		Hostname:   hostname,
		Image:      image,
		Privileged: privileged,
		Env:        copyStringMap(router.Env),
		Loopbacks:  append([]string(nil), router.Loopbacks...),
		Mounts:     mounts,
		Sysctls:    mergedSysctls(defaultRouterSysctls(), mergedSysctls(t.Lab.Defaults.Sysctls, router.Sysctls)),
		Linux:      copyLinux(router.Linux),
		Commands:   append([]Command(nil), router.Commands...),
	}
}

// ResolveRouters resolves every router definition in the topology.
func (t *Topology) ResolveRouters() map[string]ResolvedRouter {
	resolved := make(map[string]ResolvedRouter, len(t.Routers))
	for name := range t.Routers {
		resolved[name] = t.ResolveRouter(name)
	}
	return resolved
}

// SourcePath returns the absolute path of the topology file on disk.
func (t *Topology) SourcePath() string {
	return t.sourcePath
}

// BaseDir returns the directory containing the topology file.
func (t *Topology) BaseDir() string {
	return t.baseDir
}

// WorkDirPath returns the absolute workspace directory used for generated lab
// state, defaulting to DefaultWorkDir relative to the topology.
func (t *Topology) WorkDirPath() string {
	return ResolveWorkDir(t.baseDir, t.Lab.WorkDir)
}

// LabDir returns the per-lab working directory below WorkDirPath.
func (t *Topology) LabDir() string {
	return filepath.Join(t.WorkDirPath(), "labs", t.Lab.Name)
}

// RoutersDir returns the directory containing per-router generated files.
func (t *Topology) RoutersDir() string {
	return filepath.Join(t.LabDir(), "routers")
}

// RouterDir returns the generated state directory for one router.
func (t *Topology) RouterDir(name string) string {
	return filepath.Join(t.RoutersDir(), name)
}

// RouterConfigDir returns the bind-mounted /etc/frr directory for one router.
func (t *Topology) RouterConfigDir(name string) string {
	return filepath.Join(t.RouterDir(name), "etc", "frr")
}

// RouterSeedMarkerPath returns the marker file used to skip one-time vtysh
// seeding on later restarts.
func (t *Topology) RouterSeedMarkerPath(name string) string {
	return filepath.Join(t.RouterDir(name), ".seeded-vtysh")
}

// StatePath returns the JSON runtime snapshot path for this lab.
func (t *Topology) StatePath() string {
	return filepath.Join(t.LabDir(), "state.json")
}

// ResolvePath resolves a host path relative to the topology file.
func (t *Topology) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(t.baseDir, path)
}

// ResolveWorkDir computes the generated-state root for one topology base
// directory, honoring WorkDirOverrideEnv before the YAML field.
func ResolveWorkDir(baseDir, configured string) string {
	workDir := strings.TrimSpace(os.Getenv(WorkDirOverrideEnv))
	if workDir == "" {
		workDir = strings.TrimSpace(configured)
	}
	if workDir == "" {
		workDir = DefaultWorkDir
	}
	if filepath.IsAbs(workDir) {
		return workDir
	}
	return filepath.Join(baseDir, workDir)
}

// Digest returns a stable hash of the topology content used in Docker labels
// and runtime state bookkeeping.
func (t *Topology) Digest() (string, error) {
	payload, err := json.Marshal(struct {
		APIVersion string            `json:"apiVersion"`
		Lab        Lab               `json:"lab"`
		Routers    map[string]Router `json:"routers"`
		Links      []Link            `json:"links"`
		Pings      []Ping            `json:"pings,omitempty"`
	}{
		APIVersion: t.APIVersion,
		Lab:        t.Lab,
		Routers:    t.Routers,
		Links:      t.Links,
		Pings:      t.Pings,
	})
	if err != nil {
		return "", fmt.Errorf("marshal topology digest payload: %w", err)
	}

	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

func validateLinux(routerName string, linux Linux) error {
	bridgeNames := make(map[string]struct{}, len(linux.Bridges))
	namespaceNames := make(map[string]struct{})
	vxlanNames := make(map[string]struct{})
	bridgeInterfaces := make(map[string]string)

	for _, route := range linux.Routes {
		if strings.TrimSpace(route.To) == "" {
			return fmt.Errorf("router %q has a linux route with empty to", routerName)
		}
		if strings.TrimSpace(route.Via) == "" && strings.TrimSpace(route.Dev) == "" {
			return fmt.Errorf("router %q route %q must set via, dev, or both", routerName, route.To)
		}
		if route.Via != "" && net.ParseIP(route.Via) == nil {
			return fmt.Errorf("router %q route %q has invalid via %q", routerName, route.To, route.Via)
		}
	}

	for _, bridge := range linux.Bridges {
		if strings.TrimSpace(bridge.Name) == "" {
			return fmt.Errorf("router %q has a linux bridge with empty name", routerName)
		}
		if _, ok := bridgeNames[bridge.Name]; ok {
			return fmt.Errorf("router %q reuses linux bridge name %q", routerName, bridge.Name)
		}
		bridgeNames[bridge.Name] = struct{}{}

		for _, address := range bridge.Addresses {
			if _, _, err := net.ParseCIDR(address); err != nil {
				return fmt.Errorf("router %q bridge %q has invalid address %q: %w", routerName, bridge.Name, address, err)
			}
		}
		for _, iface := range bridge.Interfaces {
			if strings.TrimSpace(iface) == "" {
				return fmt.Errorf("router %q bridge %q references an empty interface", routerName, bridge.Name)
			}
			if owner, ok := bridgeInterfaces[iface]; ok {
				return fmt.Errorf("router %q reuses interface %q across linux bridges %q and %q", routerName, iface, owner, bridge.Name)
			}
			bridgeInterfaces[iface] = bridge.Name
		}
		for _, vxlan := range bridge.VXLANS {
			if strings.TrimSpace(vxlan.Name) == "" {
				return fmt.Errorf("router %q bridge %q has a vxlan with empty name", routerName, bridge.Name)
			}
			if _, ok := vxlanNames[vxlan.Name]; ok {
				return fmt.Errorf("router %q reuses vxlan name %q", routerName, vxlan.Name)
			}
			vxlanNames[vxlan.Name] = struct{}{}
			if vxlan.VNI <= 0 || vxlan.VNI > 16777215 {
				return fmt.Errorf("router %q bridge %q vxlan %q has invalid vni %d", routerName, bridge.Name, vxlan.Name, vxlan.VNI)
			}
			if vxlan.Local != "" && net.ParseIP(vxlan.Local) == nil {
				return fmt.Errorf("router %q bridge %q vxlan %q has invalid local %q", routerName, bridge.Name, vxlan.Name, vxlan.Local)
			}
			if vxlan.DstPort < 0 || vxlan.DstPort > 65535 {
				return fmt.Errorf("router %q bridge %q vxlan %q has invalid dstport %d", routerName, bridge.Name, vxlan.Name, vxlan.DstPort)
			}
		}
		for _, namespace := range bridge.Namespaces {
			if strings.TrimSpace(namespace.Name) == "" {
				return fmt.Errorf("router %q bridge %q has a namespace with empty name", routerName, bridge.Name)
			}
			if _, ok := namespaceNames[namespace.Name]; ok {
				return fmt.Errorf("router %q reuses namespace name %q", routerName, namespace.Name)
			}
			namespaceNames[namespace.Name] = struct{}{}
			if strings.TrimSpace(namespace.IfName) == "" {
				return fmt.Errorf("router %q bridge %q namespace %q has empty ifname", routerName, bridge.Name, namespace.Name)
			}
			if namespace.MAC != "" {
				if _, err := net.ParseMAC(namespace.MAC); err != nil {
					return fmt.Errorf("router %q bridge %q namespace %q has invalid mac %q: %w", routerName, bridge.Name, namespace.Name, namespace.MAC, err)
				}
			}
			for _, address := range namespace.Addresses {
				if _, _, err := net.ParseCIDR(address); err != nil {
					return fmt.Errorf("router %q bridge %q namespace %q has invalid address %q: %w", routerName, bridge.Name, namespace.Name, address, err)
				}
			}
			if namespace.DefaultVia != "" && net.ParseIP(namespace.DefaultVia) == nil {
				return fmt.Errorf("router %q bridge %q namespace %q has invalid defaultVia %q", routerName, bridge.Name, namespace.Name, namespace.DefaultVia)
			}
		}
	}

	return nil
}

func copyStringMap(input map[string]string) map[string]string {
	return maps.Clone(input)
}

func defaultRouterSysctls() map[string]string {
	return map[string]string{
		"net.ipv4.ip_forward":         "1",
		"net.ipv4.conf.all.rp_filter": "0",
	}
}

func copyLinux(linux Linux) Linux {
	result := Linux{
		Routes:  append([]Route(nil), linux.Routes...),
		Bridges: make([]Bridge, 0, len(linux.Bridges)),
	}
	for _, bridge := range linux.Bridges {
		copyBridge := Bridge{
			Name:       bridge.Name,
			Addresses:  append([]string(nil), bridge.Addresses...),
			Interfaces: append([]string(nil), bridge.Interfaces...),
			VXLANS:     append([]VXLAN(nil), bridge.VXLANS...),
			Namespaces: make([]Namespace, 0, len(bridge.Namespaces)),
		}
		for _, namespace := range bridge.Namespaces {
			copyBridge.Namespaces = append(copyBridge.Namespaces, Namespace{
				Name:       namespace.Name,
				IfName:     namespace.IfName,
				MAC:        namespace.MAC,
				Addresses:  append([]string(nil), namespace.Addresses...),
				DefaultVia: namespace.DefaultVia,
			})
		}
		result.Bridges = append(result.Bridges, copyBridge)
	}
	return result
}

func mergedSysctls(defaults, overrides map[string]string) map[string]string {
	if len(defaults) == 0 && len(overrides) == 0 {
		return nil
	}

	merged := make(map[string]string, len(defaults)+len(overrides))
	maps.Copy(merged, defaults)
	maps.Copy(merged, overrides)
	return merged
}
