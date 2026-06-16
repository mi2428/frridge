package frr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRouterFilesSeedsOnFirstRunOnly(t *testing.T) {
	t.Parallel()

	routerDir := filepath.Join(t.TempDir(), "r1")
	first, err := PrepareRouterFiles(routerDir, "r1", false)
	if err != nil {
		t.Fatalf("PrepareRouterFiles(first) error = %v", err)
	}
	if !first.NeedsSeed {
		t.Fatalf("PrepareRouterFiles(first).NeedsSeed = false, want true")
	}

	if err := MarkSeeded(first.MarkerPath); err != nil {
		t.Fatalf("MarkSeeded() error = %v", err)
	}

	second, err := PrepareRouterFiles(routerDir, "r1", false)
	if err != nil {
		t.Fatalf("PrepareRouterFiles(second) error = %v", err)
	}
	if second.NeedsSeed {
		t.Fatalf("PrepareRouterFiles(second).NeedsSeed = true, want false")
	}
}

func TestPrepareRouterFilesReseedRewritesBaseConfig(t *testing.T) {
	t.Parallel()

	routerDir := filepath.Join(t.TempDir(), "r1")
	prepared, err := PrepareRouterFiles(routerDir, "old-host", false)
	if err != nil {
		t.Fatalf("PrepareRouterFiles() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(prepared.ConfigDir, "frr.conf"), []byte("custom\n"), 0o640); err != nil {
		t.Fatalf("overwrite frr.conf: %v", err)
	}
	if err := MarkSeeded(prepared.MarkerPath); err != nil {
		t.Fatalf("MarkSeeded() error = %v", err)
	}

	reseeded, err := PrepareRouterFiles(routerDir, "new-host", true)
	if err != nil {
		t.Fatalf("PrepareRouterFiles(reseed) error = %v", err)
	}
	if !reseeded.NeedsSeed {
		t.Fatalf("PrepareRouterFiles(reseed).NeedsSeed = false, want true")
	}

	data, err := os.ReadFile(filepath.Join(reseeded.ConfigDir, "frr.conf"))
	if err != nil {
		t.Fatalf("read frr.conf: %v", err)
	}
	if !strings.Contains(string(data), "hostname new-host") {
		t.Fatalf("frr.conf = %q, want reseeded hostname", string(data))
	}
}

func TestDefaultDaemonsMatchesFRRContainerExpectations(t *testing.T) {
	t.Parallel()

	daemons := DefaultDaemons()
	for _, want := range []string{
		"zebra=yes",
		"bgpd=yes",
		"ospfd=yes",
		"ospf6d=yes",
		"isisd=yes",
		"staticd=yes",
		"vtysh_enable=yes",
		`zebra_options="  -A 127.0.0.1 -s 90000000"`,
		`bgpd_options="   -A 127.0.0.1"`,
		`staticd_options="-A 127.0.0.1"`,
		`frr_profile="datacenter"`,
	} {
		if !strings.Contains(daemons, want) {
			t.Fatalf("DefaultDaemons() missing %q in %q", want, daemons)
		}
	}
}
