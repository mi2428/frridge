// Package netlink owns the host-side bridge/veth wiring plus router main-netns
// dataplane configuration.
package netlink

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"frridge/internal/config"
	vnetlink "github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// Manager abstracts the host networking primitives required by the runtime.
type Manager interface {
	EnsureBridge(name string, mtu int) error
	CreateBridgeAttachment(bridgeName, hostIfName, peerIfName string, peerPID, mtu int) error
	CreateP2PAttachment(aIfName string, aPID int, bIfName string, bPID int, mtu int) error
	ConfigureAttachedLink(pid int, tempIfName string, member config.LinkMember, mtu int) error
	ConfigureLoopback(pid int, addresses []string) error
	ConfigureVRF(pid int, vrf config.VRF) error
	ConfigureBond(pid int, bond config.Bond) error
	ConfigureBridge(pid int, bridge config.Bridge) error
	AttachLinkMaster(pid int, ifName, master string) error
	ConfigureInterface(pid int, iface config.Interface) error
	ConfigureVeth(pid int, veth config.Veth) error
	ConfigureBridgeNamespaceVeth(pid int, bridgeName, hostVeth, peerVeth string) error
	ConfigureRoute(pid int, route config.Route) error
	DeleteLink(name string) error
}

type manager struct{}

type linkConfig struct {
	renameTo    string
	master      string
	mac         string
	addrGenMode string
	addresses   []string
	mtu         int
	up          bool
}

const (
	ipv6AddrGenModeEUI64 = iota
	ipv6AddrGenModeNone
	ipv6AddrGenModeStableSecret
	ipv6AddrGenModeRandom
)

// New returns a netlink-backed networking manager.
func New() Manager {
	return &manager{}
}

func (m *manager) EnsureBridge(name string, mtu int) error {
	link, err := vnetlink.LinkByName(name)
	if err != nil {
		if _, ok := errors.AsType[vnetlink.LinkNotFoundError](err); !ok {
			return fmt.Errorf("lookup bridge %s: %w", name, err)
		}

		attrs := vnetlink.NewLinkAttrs()
		attrs.Name = name
		if mtu > 0 {
			attrs.MTU = mtu
		}
		bridge := &vnetlink.Bridge{LinkAttrs: attrs}
		if err := vnetlink.LinkAdd(bridge); err != nil {
			return fmt.Errorf("create bridge %s: %w", name, err)
		}
		link = bridge
	}

	if mtu > 0 && link.Attrs().MTU != mtu {
		if err := vnetlink.LinkSetMTU(link, mtu); err != nil {
			return fmt.Errorf("set bridge mtu %s: %w", name, err)
		}
	}
	if err := vnetlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring bridge %s up: %w", name, err)
	}
	return nil
}

func (m *manager) CreateBridgeAttachment(bridgeName, hostIfName, peerIfName string, peerPID, mtu int) error {
	if err := m.createVeth(hostIfName, peerIfName, mtu); err != nil {
		return err
	}

	hostLink, err := vnetlink.LinkByName(hostIfName)
	if err != nil {
		return fmt.Errorf("lookup host veth %s: %w", hostIfName, err)
	}
	peerLink, err := vnetlink.LinkByName(peerIfName)
	if err != nil {
		return fmt.Errorf("lookup peer veth %s: %w", peerIfName, err)
	}
	bridgeLink, err := vnetlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("lookup bridge %s: %w", bridgeName, err)
	}
	bridge, ok := bridgeLink.(*vnetlink.Bridge)
	if !ok {
		return fmt.Errorf("%s exists but is not a bridge", bridgeName)
	}

	if err := vnetlink.LinkSetMaster(hostLink, bridge); err != nil {
		return fmt.Errorf("attach %s to bridge %s: %w", hostIfName, bridgeName, err)
	}
	if err := vnetlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("bring host veth %s up: %w", hostIfName, err)
	}
	if err := vnetlink.LinkSetNsPid(peerLink, peerPID); err != nil {
		return fmt.Errorf("move %s to pid %d: %w", peerIfName, peerPID, err)
	}
	return nil
}

func (m *manager) CreateP2PAttachment(aIfName string, aPID int, bIfName string, bPID int, mtu int) error {
	if err := m.createVeth(aIfName, bIfName, mtu); err != nil {
		return err
	}

	aLink, err := vnetlink.LinkByName(aIfName)
	if err != nil {
		return fmt.Errorf("lookup p2p veth %s: %w", aIfName, err)
	}
	bLink, err := vnetlink.LinkByName(bIfName)
	if err != nil {
		return fmt.Errorf("lookup p2p veth %s: %w", bIfName, err)
	}

	if err := vnetlink.LinkSetNsPid(aLink, aPID); err != nil {
		return fmt.Errorf("move %s to pid %d: %w", aIfName, aPID, err)
	}
	if err := vnetlink.LinkSetNsPid(bLink, bPID); err != nil {
		return fmt.Errorf("move %s to pid %d: %w", bIfName, bPID, err)
	}
	return nil
}

func (m *manager) ConfigureAttachedLink(pid int, tempIfName string, member config.LinkMember, mtu int) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		return m.configureLink(handle, tempIfName, linkConfig{
			renameTo:  member.IfName,
			mtu:       mtu,
			mac:       member.MAC,
			addresses: nonEmptyStrings(member.IPv4),
			up:        true,
		})
	})
}

func (m *manager) ConfigureLoopback(pid int, addresses []string) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		link, err := lookupLink(handle, "lo")
		if err != nil {
			return err
		}
		if err := handle.LinkSetUp(link); err != nil {
			return fmt.Errorf("bring loopback up: %w", err)
		}
		for _, address := range addresses {
			addr, err := parseAddr(address)
			if err != nil {
				return err
			}
			if err := handleAddrReplace(handle, link, addr); err != nil {
				return fmt.Errorf("configure loopback address %s: %w", address, err)
			}
		}
		return nil
	})
}

func (m *manager) ConfigureVRF(pid int, vrf config.VRF) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		attrs := vnetlink.NewLinkAttrs()
		attrs.Name = vrf.Name
		if err := handle.LinkAdd(&vnetlink.Vrf{
			LinkAttrs: attrs,
			Table:     uint32(vrf.Table),
		}); err != nil {
			return fmt.Errorf("create vrf %s: %w", vrf.Name, err)
		}
		return m.configureLink(handle, vrf.Name, linkConfig{up: true})
	})
}

func (m *manager) ConfigureBond(pid int, bond config.Bond) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		attrs := vnetlink.NewLinkAttrs()
		attrs.Name = bond.Name

		link := vnetlink.NewLinkBond(attrs)
		link.Mode = vnetlink.StringToBondMode(bond.Mode)
		if err := handle.LinkAdd(link); err != nil {
			return fmt.Errorf("create bond %s: %w", bond.Name, err)
		}
		if err := m.configureLink(handle, bond.Name, linkConfig{
			mac:         bond.MAC,
			addrGenMode: bond.AddrGenMode,
			addresses:   bond.Addresses,
		}); err != nil {
			return err
		}

		for _, iface := range bond.Interfaces {
			if err := setLinkUp(handle, iface, false); err != nil {
				return err
			}
			if err := m.attachLinkMaster(handle, iface, bond.Name); err != nil {
				return err
			}
			if err := setLinkUp(handle, iface, true); err != nil {
				return err
			}
		}
		return setLinkUp(handle, bond.Name, true)
	})
}

func (m *manager) ConfigureBridge(pid int, bridge config.Bridge) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		attrs := vnetlink.NewLinkAttrs()
		attrs.Name = bridge.Name
		if err := handle.LinkAdd(&vnetlink.Bridge{LinkAttrs: attrs}); err != nil {
			return fmt.Errorf("create bridge %s: %w", bridge.Name, err)
		}
		if err := m.configureLink(handle, bridge.Name, linkConfig{
			master:      bridge.Master,
			mac:         bridge.MAC,
			addrGenMode: bridge.AddrGenMode,
			addresses:   bridge.Addresses,
			up:          true,
		}); err != nil {
			return err
		}

		for _, iface := range bridge.Interfaces {
			if err := m.attachLinkMaster(handle, iface, bridge.Name); err != nil {
				return err
			}
			if err := setLinkUp(handle, iface, true); err != nil {
				return err
			}
		}
		for _, vxlan := range bridge.VXLANS {
			if err := m.configureVXLAN(handle, bridge.Name, vxlan); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *manager) AttachLinkMaster(pid int, ifName, master string) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		return m.attachLinkMaster(handle, ifName, master)
	})
}

func (m *manager) ConfigureInterface(pid int, iface config.Interface) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		return m.configureLink(handle, iface.Name, linkConfig{
			master:      iface.Master,
			mac:         iface.MAC,
			addrGenMode: iface.AddrGenMode,
			addresses:   iface.Addresses,
			up:          true,
		})
	})
}

func (m *manager) ConfigureVeth(pid int, veth config.Veth) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		if err := addVeth(handle, veth.Name, veth.Peer, 0); err != nil {
			return fmt.Errorf("create veth pair %s/%s: %w", veth.Name, veth.Peer, err)
		}
		return m.configureLink(handle, veth.Name, linkConfig{
			master:      veth.Master,
			mac:         veth.MAC,
			addrGenMode: veth.AddrGenMode,
			addresses:   veth.Addresses,
			up:          true,
		})
	})
}

func (m *manager) ConfigureBridgeNamespaceVeth(pid int, bridgeName, hostVeth, peerVeth string) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		if err := addVeth(handle, hostVeth, peerVeth, 0); err != nil {
			return fmt.Errorf("create namespace veth pair %s/%s: %w", hostVeth, peerVeth, err)
		}
		return m.configureLink(handle, hostVeth, linkConfig{
			master: bridgeName,
			up:     true,
		})
	})
}

func (m *manager) ConfigureRoute(pid int, route config.Route) error {
	return m.withPIDHandle(pid, func(handle *vnetlink.Handle) error {
		netlinkRoute, err := buildRoute(handle, route)
		if err != nil {
			return err
		}
		if err := handle.RouteReplace(netlinkRoute); err != nil {
			return fmt.Errorf("configure route %s: %w", route.To, err)
		}
		return nil
	})
}

func (m *manager) DeleteLink(name string) error {
	link, err := vnetlink.LinkByName(name)
	if err != nil {
		if _, ok := errors.AsType[vnetlink.LinkNotFoundError](err); ok {
			return nil
		}
		return fmt.Errorf("lookup link %s: %w", name, err)
	}

	if err := vnetlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete link %s: %w", name, err)
	}
	return nil
}

func (m *manager) createVeth(name, peer string, mtu int) error {
	return addVeth(nil, name, peer, mtu)
}

func (m *manager) withPIDHandle(pid int, fn func(*vnetlink.Handle) error) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}

	namespace, err := netns.GetFromPid(pid)
	if err != nil {
		return fmt.Errorf("open netns for pid %d: %w", pid, err)
	}
	defer namespace.Close()

	handle, err := vnetlink.NewHandleAt(namespace)
	if err != nil {
		return fmt.Errorf("create netlink handle for pid %d: %w", pid, err)
	}
	defer handle.Close()

	return fn(handle)
}

func (m *manager) configureVXLAN(handle *vnetlink.Handle, bridgeName string, vxlan config.VXLAN) error {
	attrs := vnetlink.NewLinkAttrs()
	attrs.Name = vxlan.Name

	link := &vnetlink.Vxlan{
		LinkAttrs: attrs,
		VxlanId:   vxlan.VNI,
		Learning:  !vxlan.NoLearning,
		Port:      defaultVXLANDstPort(vxlan.DstPort),
	}
	if strings.TrimSpace(vxlan.Local) != "" {
		ip, err := parseIP(vxlan.Local)
		if err != nil {
			return fmt.Errorf("configure vxlan %s local: %w", vxlan.Name, err)
		}
		link.SrcAddr = ip
	}
	if err := handle.LinkAdd(link); err != nil {
		return fmt.Errorf("create vxlan %s: %w", vxlan.Name, err)
	}
	if err := m.configureLink(handle, vxlan.Name, linkConfig{
		master:      bridgeName,
		addrGenMode: vxlan.AddrGenMode,
	}); err != nil {
		return err
	}
	if err := configureBridgeSlave(handle, vxlan); err != nil {
		return err
	}
	return setLinkUp(handle, vxlan.Name, true)
}

func (m *manager) configureLink(handle *vnetlink.Handle, currentName string, cfg linkConfig) error {
	link, err := lookupLink(handle, currentName)
	if err != nil {
		return err
	}

	if renameTo := strings.TrimSpace(cfg.renameTo); renameTo != "" && renameTo != currentName {
		if err := handle.LinkSetName(link, renameTo); err != nil {
			return fmt.Errorf("rename link %s to %s: %w", currentName, renameTo, err)
		}
		currentName = renameTo
		link, err = lookupLink(handle, currentName)
		if err != nil {
			return err
		}
	}

	if cfg.mtu > 0 {
		if err := handle.LinkSetMTU(link, cfg.mtu); err != nil {
			return fmt.Errorf("set link %s mtu %d: %w", currentName, cfg.mtu, err)
		}
	}
	if err := m.attachLinkMaster(handle, currentName, cfg.master); err != nil {
		return err
	}
	if addrGenMode := strings.TrimSpace(cfg.addrGenMode); addrGenMode != "" {
		mode, err := parseAddrGenMode(addrGenMode)
		if err != nil {
			return fmt.Errorf("set link %s addrgenmode %s: %w", currentName, addrGenMode, err)
		}
		if err := handle.LinkSetIP6AddrGenMode(link, mode); err != nil {
			return fmt.Errorf("set link %s addrgenmode %s: %w", currentName, addrGenMode, err)
		}
	}
	if mac := strings.TrimSpace(cfg.mac); mac != "" {
		hardwareAddr, err := net.ParseMAC(mac)
		if err != nil {
			return fmt.Errorf("parse mac %q: %w", mac, err)
		}
		if err := handle.LinkSetHardwareAddr(link, hardwareAddr); err != nil {
			return fmt.Errorf("set link %s mac %s: %w", currentName, mac, err)
		}
	}
	for _, address := range cfg.addresses {
		addr, err := parseAddr(address)
		if err != nil {
			return err
		}
		if err := handleAddrReplace(handle, link, addr); err != nil {
			return fmt.Errorf("configure address %s on %s: %w", address, currentName, err)
		}
	}
	if cfg.up {
		if err := handle.LinkSetUp(link); err != nil {
			return fmt.Errorf("bring link %s up: %w", currentName, err)
		}
	}
	return nil
}

func (m *manager) attachLinkMaster(handle *vnetlink.Handle, ifName, master string) error {
	master = strings.TrimSpace(master)
	if master == "" {
		return nil
	}

	link, err := lookupLink(handle, ifName)
	if err != nil {
		return err
	}
	masterLink, err := lookupLink(handle, master)
	if err != nil {
		return err
	}
	if err := handle.LinkSetMaster(link, masterLink); err != nil {
		return fmt.Errorf("attach link %s to master %s: %w", ifName, master, err)
	}
	return nil
}

func addVeth(handle *vnetlink.Handle, name, peer string, mtu int) error {
	attrs := vnetlink.NewLinkAttrs()
	attrs.Name = name
	if mtu > 0 {
		attrs.MTU = mtu
	}

	veth := vnetlink.NewVeth(attrs)
	veth.PeerName = peer
	if mtu > 0 {
		veth.PeerMTU = uint32(mtu)
	}
	if handle != nil {
		return handle.LinkAdd(veth)
	}
	return vnetlink.LinkAdd(veth)
}

func lookupLink(handle *vnetlink.Handle, name string) (vnetlink.Link, error) {
	link, err := handle.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup link %s: %w", name, err)
	}
	return link, nil
}

func setLinkUp(handle *vnetlink.Handle, ifName string, up bool) error {
	link, err := lookupLink(handle, ifName)
	if err != nil {
		return err
	}
	if up {
		if err := handle.LinkSetUp(link); err != nil {
			return fmt.Errorf("bring link %s up: %w", ifName, err)
		}
		return nil
	}
	if err := handle.LinkSetDown(link); err != nil {
		return fmt.Errorf("bring link %s down: %w", ifName, err)
	}
	return nil
}

func configureBridgeSlave(handle *vnetlink.Handle, vxlan config.VXLAN) error {
	if vxlan.BridgeSlave.NeighSuppress == nil && vxlan.BridgeSlave.Learning == nil {
		return nil
	}

	link, err := lookupLink(handle, vxlan.Name)
	if err != nil {
		return err
	}
	if vxlan.BridgeSlave.NeighSuppress != nil {
		if err := handleLinkSetBrNeighSuppress(handle, link, *vxlan.BridgeSlave.NeighSuppress); err != nil {
			return fmt.Errorf("set vxlan %s neigh_suppress: %w", vxlan.Name, err)
		}
	}
	if vxlan.BridgeSlave.Learning != nil {
		if err := handle.LinkSetLearning(link, *vxlan.BridgeSlave.Learning); err != nil {
			return fmt.Errorf("set vxlan %s bridge learning: %w", vxlan.Name, err)
		}
	}
	return nil
}

func buildRoute(handle *vnetlink.Handle, route config.Route) (*vnetlink.Route, error) {
	_, dst, err := net.ParseCIDR(route.To)
	if err != nil {
		return nil, fmt.Errorf("parse route destination %q: %w", route.To, err)
	}

	netlinkRoute := &vnetlink.Route{Dst: dst}
	if via := strings.TrimSpace(route.Via); via != "" {
		ip, err := parseIP(via)
		if err != nil {
			return nil, fmt.Errorf("parse route via %q: %w", via, err)
		}
		netlinkRoute.Gw = ip
	}
	if dev := strings.TrimSpace(route.Dev); dev != "" {
		link, err := lookupLink(handle, dev)
		if err != nil {
			return nil, err
		}
		netlinkRoute.LinkIndex = link.Attrs().Index
	}
	return netlinkRoute, nil
}

func parseAddr(address string) (*vnetlink.Addr, error) {
	addr, err := vnetlink.ParseAddr(address)
	if err != nil {
		return nil, fmt.Errorf("parse address %q: %w", address, err)
	}
	return addr, nil
}

func parseIP(address string) (net.IP, error) {
	ip := net.ParseIP(address)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address %q", address)
	}
	return ip, nil
}

func parseAddrGenMode(mode string) (int, error) {
	switch strings.TrimSpace(mode) {
	case "eui64":
		return ipv6AddrGenModeEUI64, nil
	case "none":
		return ipv6AddrGenModeNone, nil
	case "stable_secret":
		return ipv6AddrGenModeStableSecret, nil
	case "random":
		return ipv6AddrGenModeRandom, nil
	default:
		return 0, fmt.Errorf("unsupported addrgenmode %q", mode)
	}
}

func defaultVXLANDstPort(port int) int {
	if port == 0 {
		return 4789
	}
	return port
}

func nonEmptyStrings(values ...string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
