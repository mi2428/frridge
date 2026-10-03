//go:build linux

package netlink

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"frridge/internal/config"
	vnetlink "github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

func TestLinuxDataplane(t *testing.T) {
	if os.Getenv("FRRIDGE_RUN_INTEGRATION") != "1" {
		t.Skip("set FRRIDGE_RUN_INTEGRATION=1 to run isolated Linux dataplane tests")
	}
	if os.Geteuid() != 0 {
		t.Skip("Linux dataplane tests require root")
	}
	caps := [2]unix.CapUserData{}
	requireKernelOK(t, unix.Capget(&unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}, &caps[0]))
	const required = 1<<unix.CAP_SYS_ADMIN | 1<<unix.CAP_NET_ADMIN
	if caps[0].Effective&required != required {
		t.Skip("Linux dataplane tests require CAP_SYS_ADMIN and CAP_NET_ADMIN")
	}
	if os.Getenv("FRRIDGE_NETLINK_TEST_CHILD") != "1" {
		// Re-exec the entire process: no goroutine or package-level netlink socket
		// can accidentally use the host namespace while tests run.
		original, err := os.Open("/proc/self/ns/net")
		requireKernelOK(t, err)
		defer original.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinuxDataplane$", "-test.v", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), "FRRIDGE_NETLINK_TEST_CHILD=1")
		cmd.ExtraFiles = []*os.File{original}
		cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNET, Pdeathsig: syscall.SIGKILL}
		output, err := cmd.CombinedOutput()
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("creating an isolated network namespace is not permitted: %v", err)
		}
		if err != nil {
			t.Fatalf("isolated dataplane test: %v\n%s", err, output)
		}
		t.Logf("isolated dataplane test:\n%s", output)
		return
	}

	// Fail closed if the internal child marker is set without a different,
	// valid inherited network namespace. Never open a netlink socket first.
	original := netns.NsHandle(3)
	kind, err := unix.IoctlRetInt(int(original), unix.NS_GET_NSTYPE)
	requireKernelOK(t, err)
	defer original.Close()
	if kind != unix.CLONE_NEWNET {
		t.Fatal("inherited fd is not a network namespace")
	}
	current, err := netns.Get()
	requireKernelOK(t, err)
	defer current.Close()
	if current.Equal(original) {
		t.Fatal("refusing to run dataplane tests in the original network namespace")
	}
	host, err := vnetlink.NewHandle(unix.NETLINK_ROUTE)
	requireKernelOK(t, err)
	t.Cleanup(host.Close)
	links, err := host.LinkList()
	requireKernelOK(t, err)
	if len(links) != 1 || links[0].Attrs().Name != "lo" {
		t.Fatal("isolated namespace must initially contain only loopback")
	}

	m := New()
	// Anonymous namespaces have kernel-assigned unique IDs, with no /run/netns
	// mounts. Names are scoped to this process; cleanup never sweeps lab prefixes.
	prefix := "nt" + rand.Text()[:8]
	name := func(suffix string) string { return prefix + suffix }

	t.Run("p2p_and_shared_bridge", func(t *testing.T) {
		aPID, a := newKernelPeer(t)
		bPID, b := newKernelPeer(t)
		left, right := name("pa"), name("pb")
		requireKernelOK(t, m.CreateP2PAttachment(left, aPID, right, bPID, 1400))
		for _, endpoint := range []struct {
			pid     int
			handle  *vnetlink.Handle
			temp    string
			address string
			mac     string
		}{
			{aPID, a, left, "192.0.2.0/31", "02:00:00:00:01:01"},
			{bPID, b, right, "192.0.2.1/31", "02:00:00:00:01:02"},
		} {
			requireKernelOK(t, m.ConfigureAttachedLink(endpoint.pid, endpoint.temp, config.LinkMember{
				IfName: "eth1", IPv4: endpoint.address, MAC: endpoint.mac,
			}, 1400))
			link := kernelLink(t, endpoint.handle, "eth1", "veth", 0, 1400)
			if link.Attrs().HardwareAddr.String() != endpoint.mac {
				t.Fatalf("eth1 MAC = %s, want %s", link.Attrs().HardwareAddr, endpoint.mac)
			}
			kernelAddresses(t, endpoint.handle, link, endpoint.address)
			kernelAbsent(t, endpoint.handle, left, right)
		}
		kernelAbsent(t, host, left, right, "eth1")
		leftLink, err := a.LinkByName("eth1")
		requireKernelOK(t, err)
		requireKernelOK(t, a.LinkDel(leftLink))
		kernelAbsent(t, a, "eth1")
		kernelAbsent(t, b, "eth1")

		bridge, portA, portB, peerA, peerB := name("br"), name("ha"), name("hb"), name("qa"), name("qb")
		cleanupKernelLinks(t, m, portA, portB, peerA, peerB, bridge)
		requireKernelOK(t, m.EnsureBridge(bridge, 1500))
		requireKernelOK(t, m.EnsureBridge(bridge, 1400)) // Existing bridge is reconciled, not duplicated.
		br := kernelLink(t, host, bridge, "bridge", 0, 1400)
		for _, endpoint := range []struct {
			pid    int
			handle *vnetlink.Handle
			port   string
			peer   string
		}{
			{aPID, a, portA, peerA}, {bPID, b, portB, peerB},
		} {
			requireKernelOK(t, m.CreateBridgeAttachment(bridge, endpoint.port, endpoint.peer, endpoint.pid, 1400))
			requireKernelOK(t, m.ConfigureAttachedLink(endpoint.pid, endpoint.peer, config.LinkMember{IfName: "eth2"}, 1400))
			kernelLink(t, host, endpoint.port, "veth", br.Attrs().Index, 1400)
			kernelLink(t, endpoint.handle, "eth2", "veth", 0, 1400)
			kernelAbsent(t, endpoint.handle, bridge, portA, portB, peerA, peerB)
			kernelAbsent(t, host, endpoint.peer)
		}
		// Both ports must coexist on the same shared segment before teardown.
		kernelLink(t, host, portA, "veth", br.Attrs().Index, 1400)
		kernelLink(t, host, portB, "veth", br.Attrs().Index, 1400)
		requireKernelOK(t, m.DeleteLink(portA))
		requireKernelOK(t, m.DeleteLink(portB))
		kernelAbsent(t, a, "eth2")
		kernelAbsent(t, b, "eth2")
		requireKernelOK(t, m.DeleteLink(bridge))
		requireKernelOK(t, m.DeleteLink(bridge)) // Cleanup is idempotent.
		kernelAbsent(t, host, bridge, portA, portB, peerA, peerB)
	})

	t.Run("vrf_bond_vxlan_and_dual_stack", func(t *testing.T) {
		pid := os.Getpid()
		vrf, bond, bridge, vxlan := name("vr"), name("bo"), name("db"), name("vx")
		slave, peer, routed, routePeer := name("sl"), name("sp"), name("rt"), name("rp")
		cleanupKernelLinks(t, m, vxlan, bridge, bond, slave, peer, routed, routePeer, vrf)
		requireKernelOK(t, m.ConfigureLoopback(pid, []string{"10.255.0.1/32", "2001:db8:ffff::1/128"}))
		lo := kernelLink(t, host, "lo", "device", 0, 65536)
		kernelAddresses(t, host, lo, "10.255.0.1/32", "2001:db8:ffff::1/128")
		requireKernelOK(t, m.ConfigureVRF(pid, config.VRF{Name: vrf, Table: 1100}))
		requireKernelOK(t, m.ConfigureVeth(pid, config.Veth{Name: slave, Peer: peer}))
		requireKernelOK(t, m.ConfigureBond(pid, config.Bond{Name: bond, Mode: "active-backup", Interfaces: []string{slave}}))
		learning, suppress := false, true
		requireKernelOK(t, m.ConfigureBridge(pid, config.Bridge{
			Name: bridge, Master: vrf, Interfaces: []string{bond}, AddrGenMode: "none",
			VXLANS: []config.VXLAN{{
				Name: vxlan, VNI: 100, Local: "10.255.0.1", NoLearning: true, AddrGenMode: "none",
				BridgeSlave: config.BridgeSlaveOptions{Learning: &learning, NeighSuppress: &suppress},
			}},
		}))
		vr := kernelLink(t, host, vrf, "vrf", 0, 0).(*vnetlink.Vrf)
		if vr.Table != 1100 {
			t.Fatalf("VRF table = %d, want 1100", vr.Table)
		}
		br := kernelLink(t, host, bridge, "bridge", vr.Attrs().Index, 0)
		bo := kernelLink(t, host, bond, "bond", br.Attrs().Index, 0).(*vnetlink.Bond)
		if bo.Mode != vnetlink.BOND_MODE_ACTIVE_BACKUP {
			t.Fatalf("bond mode = %v, want active-backup", bo.Mode)
		}
		kernelLink(t, host, slave, "veth", bo.Attrs().Index, 0)
		vx := kernelLink(t, host, vxlan, "vxlan", br.Attrs().Index, 0).(*vnetlink.Vxlan)
		if vx.VxlanId != 100 || vx.Port != 4789 || vx.Learning || !vx.SrcAddr.Equal(net.ParseIP("10.255.0.1")) {
			t.Fatalf("unexpected VXLAN attributes: %+v", vx)
		}
		info, err := host.LinkGetProtinfo(vx)
		requireKernelOK(t, err)
		if info.Learning || !info.NeighSuppress {
			t.Fatalf("unexpected VXLAN bridge-slave attributes: %+v", info)
		}

		requireKernelOK(t, m.ConfigureVeth(pid, config.Veth{Name: routed, Peer: routePeer}))
		requireKernelOK(t, m.ConfigureInterface(pid, config.Interface{Name: routePeer, AddrGenMode: "none"}))
		addresses := []string{"192.0.2.1/24", "2001:db8:1::1/64"}
		requireKernelOK(t, m.ConfigureInterface(pid, config.Interface{Name: routed, AddrGenMode: "none", Addresses: addresses}))
		link := kernelLink(t, host, routed, "veth", 0, 0)
		kernelAddresses(t, host, link, addresses...)
		for _, route := range []config.Route{
			{To: "198.51.100.0/24", Via: "192.0.2.2", Dev: routed},
			{To: "2001:db8:2::/64", Via: "2001:db8:1::2", Dev: routed},
		} {
			requireKernelOK(t, m.ConfigureRoute(pid, route))
			requireKernelOK(t, m.ConfigureRoute(pid, route)) // RouteReplace must not duplicate routes.
			routes, err := host.RouteList(link, vnetlink.FAMILY_ALL)
			requireKernelOK(t, err)
			found := 0
			for _, actual := range routes {
				if actual.Dst != nil && actual.Dst.String() == route.To && actual.Gw.Equal(net.ParseIP(route.Via)) && actual.LinkIndex == link.Attrs().Index && actual.Table == unix.RT_TABLE_MAIN {
					found++
				}
			}
			if found != 1 {
				t.Fatalf("route %+v has %d kernel matches, want 1: %+v", route, found, routes)
			}
		}
		requireKernelOK(t, m.DeleteLink(routed))
		kernelAbsent(t, host, routed, routePeer)
		routes, err := host.RouteList(nil, vnetlink.FAMILY_ALL)
		requireKernelOK(t, err)
		for _, route := range routes {
			if route.LinkIndex == link.Attrs().Index {
				t.Fatalf("route survived device cleanup: %+v", route)
			}
		}
	})

	t.Run("partial_failure_cleanup", func(t *testing.T) {
		sentinel, left, right := name("keep"), name("fa"), name("fb")
		cleanupKernelLinks(t, m, sentinel)
		requireKernelOK(t, m.EnsureBridge(sentinel, 1400))
		before := kernelLink(t, host, sentinel, "bridge", 0, 1400).Attrs().Index
		t.Run("missing_bridge_after_veth_creation", func(t *testing.T) {
			cleanupKernelLinks(t, m, left, right)
			err := m.CreateBridgeAttachment(name("gone"), left, right, os.Getpid(), 1400)
			if _, ok := errors.AsType[vnetlink.LinkNotFoundError](err); !ok {
				t.Fatalf("missing bridge error = %v, want LinkNotFoundError", err)
			}
			// The manager does not promise automatic rollback; exercise its public
			// cleanup primitive against the actual pair left by partial creation.
			for _, iface := range []string{left, right} {
				_, err := host.LinkByName(iface)
				requireKernelOK(t, err)
			}
		})
		kernelAbsent(t, host, left, right)
		if kernelLink(t, host, sentinel, "bridge", 0, 1400).Attrs().Index != before {
			t.Fatal("partial-failure cleanup replaced the unrelated bridge")
		}
	})
}

func newKernelPeer(t *testing.T) (int, *vnetlink.Handle) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNET, Pdeathsig: syscall.SIGKILL}
	requireKernelOK(t, cmd.Start())
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("kill namespace peer: %v", err)
		}
		_ = cmd.Wait() // SIGKILL/context cancellation is the expected teardown.
	})
	ns, err := netns.GetFromPid(cmd.Process.Pid)
	requireKernelOK(t, err)
	defer ns.Close()
	current, err := netns.Get()
	requireKernelOK(t, err)
	defer current.Close()
	if ns.Equal(current) {
		t.Fatal("peer must have its own network namespace")
	}
	handle, err := vnetlink.NewHandleAt(ns, unix.NETLINK_ROUTE)
	requireKernelOK(t, err)
	t.Cleanup(handle.Close)
	return cmd.Process.Pid, handle
}

func cleanupKernelLinks(t *testing.T, m Manager, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range names {
			if err := m.DeleteLink(name); err != nil {
				t.Errorf("cleanup %s: %v", name, err)
			}
		}
	})
}

func requireKernelOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func kernelLink(t *testing.T, handle *vnetlink.Handle, name, kind string, master, mtu int) vnetlink.Link {
	t.Helper()
	link, err := handle.LinkByName(name)
	requireKernelOK(t, err)
	attrs := link.Attrs()
	if link.Type() != kind || attrs.MasterIndex != master || attrs.Flags&net.FlagUp == 0 || (mtu > 0 && attrs.MTU != mtu) {
		t.Fatalf("%s: type=%s attrs=%+v, want type=%s master=%d up=true mtu=%d (0=any)", name, link.Type(), attrs, kind, master, mtu)
	}
	return link
}

func kernelAbsent(t *testing.T, handle *vnetlink.Handle, names ...string) {
	t.Helper()
	for _, name := range names {
		_, err := handle.LinkByName(name)
		if _, ok := errors.AsType[vnetlink.LinkNotFoundError](err); !ok {
			t.Fatalf("%s still exists or lookup failed unexpectedly: %v", name, err)
		}
	}
}

func kernelAddresses(t *testing.T, handle *vnetlink.Handle, link vnetlink.Link, want ...string) {
	t.Helper()
	addresses, err := handle.AddrList(link, vnetlink.FAMILY_ALL)
	requireKernelOK(t, err)
	for _, expected := range want {
		found := false
		for _, actual := range addresses {
			if actual.IPNet.String() == expected {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s missing address %s: %+v", link.Attrs().Name, expected, addresses)
		}
	}
}
