// Package frr owns the generated files that are bind-mounted into each router's
// /etc/frr directory.
package frr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const seedMarkerName = ".seeded-vtysh"

// PrepareResult describes the generated file locations for one router and
// whether first-boot vtysh seeding still needs to run.
type PrepareResult struct {
	ConfigDir  string
	NeedsSeed  bool
	MarkerPath string
}

// PrepareRouterFiles keeps the generated base files idempotent and uses a
// marker file to distinguish a first boot from a normal restart.
func PrepareRouterFiles(routerDir, hostname string, reseed bool) (PrepareResult, error) {
	configDir := filepath.Join(routerDir, "etc", "frr")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return PrepareResult{}, fmt.Errorf("create router config dir: %w", err)
	}

	markerPath := filepath.Join(routerDir, seedMarkerName)
	if err := writeIfMissing(filepath.Join(configDir, "daemons"), []byte(DefaultDaemons())); err != nil {
		return PrepareResult{}, err
	}
	if err := writeIfMissing(filepath.Join(configDir, "vtysh.conf"), []byte(DefaultVtyshConf())); err != nil {
		return PrepareResult{}, err
	}

	frrConfPath := filepath.Join(configDir, "frr.conf")
	needsSeed := reseed
	if reseed {
		if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
			return PrepareResult{}, fmt.Errorf("remove seed marker: %w", err)
		}
		if err := os.WriteFile(frrConfPath, []byte(BaseConfig(hostname)), 0o640); err != nil {
			return PrepareResult{}, fmt.Errorf("rewrite frr.conf: %w", err)
		}
	} else if _, err := os.Stat(frrConfPath); os.IsNotExist(err) {
		if err := os.WriteFile(frrConfPath, []byte(BaseConfig(hostname)), 0o640); err != nil {
			return PrepareResult{}, fmt.Errorf("write default frr.conf: %w", err)
		}
	}

	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		needsSeed = true
	} else if err != nil {
		return PrepareResult{}, fmt.Errorf("stat seed marker: %w", err)
	}

	return PrepareResult{
		ConfigDir:  configDir,
		NeedsSeed:  needsSeed,
		MarkerPath: markerPath,
	}, nil
}

// MarkSeeded records that the router's one-time vtysh seed commands already
// ran successfully.
func MarkSeeded(markerPath string) error {
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return fmt.Errorf("create seed marker dir: %w", err)
	}
	if err := os.WriteFile(markerPath, []byte("seeded\n"), 0o644); err != nil {
		return fmt.Errorf("write seed marker: %w", err)
	}
	return nil
}

// DefaultDaemons returns the FRR daemons file used for generated labs.
func DefaultDaemons() string {
	return strings.TrimLeft(`
zebra=yes
bgpd=yes
ospfd=no
ospf6d=no
ripd=no
ripngd=no
isisd=no
pimd=no
pim6d=no
ldpd=no
nhrpd=no
eigrpd=no
babeld=no
sharpd=no
pbrd=no
bfdd=no
fabricd=no
pathd=no
vrrpd=no
mgmtd=no

vtysh_enable=yes
zebra_options="  -A 127.0.0.1 -s 90000000"
bgpd_options="   -A 127.0.0.1"
ospfd_options="  -A 127.0.0.1"
ospf6d_options=" -A ::1"
ripd_options="   -A 127.0.0.1"
ripngd_options=" -A ::1"
isisd_options="  -A 127.0.0.1"
pimd_options="   -A 127.0.0.1"
pim6d_options="  -A ::1"
ldpd_options="   -A 127.0.0.1"
nhrpd_options="  -A 127.0.0.1"
eigrpd_options=" -A 127.0.0.1"
babeld_options=" -A 127.0.0.1"
sharpd_options=" -A 127.0.0.1"
pbrd_options="   -A 127.0.0.1"
staticd_options="-A 127.0.0.1"
bfdd_options="   -A 127.0.0.1"
fabricd_options="-A 127.0.0.1"
vrrpd_options="  -A 127.0.0.1"
pathd_options="  -A 127.0.0.1"

frr_profile="datacenter"
	`, "\n")
}

// DefaultVtyshConf enables integrated configuration mode for generated labs.
func DefaultVtyshConf() string {
	return strings.TrimLeft(`
service integrated-vtysh-config
	`, "\n")
}

// BaseConfig returns the initial frr.conf content before any optional vtysh
// seed commands are applied.
func BaseConfig(hostname string) string {
	if strings.TrimSpace(hostname) == "" {
		hostname = "frridge"
	}

	return fmt.Sprintf(strings.TrimLeft(`
frr defaults datacenter
hostname %s
service integrated-vtysh-config
!
`, "\n"), hostname)
}

func writeIfMissing(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", filepath.Base(path), err)
	}

	if err := os.WriteFile(path, data, 0o640); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
