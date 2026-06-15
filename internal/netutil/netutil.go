// Package netutil owns the host-side bridge and veth operations used to wire
// containers together.
package netutil

import (
	"errors"
	"fmt"

	"github.com/vishvananda/netlink"
)

// Manager abstracts the host networking primitives required by the runtime.
type Manager interface {
	EnsureBridge(name string, mtu int) error
	CreateBridgeAttachment(bridgeName, hostIfName, peerIfName string, peerPID, mtu int) error
	CreateP2PAttachment(aIfName string, aPID int, bIfName string, bPID int, mtu int) error
	DeleteLink(name string) error
}

// NetlinkManager implements Manager using github.com/vishvananda/netlink.
type NetlinkManager struct{}

// New returns a netlink-backed networking manager.
func New() *NetlinkManager {
	return &NetlinkManager{}
}

func (m *NetlinkManager) EnsureBridge(name string, mtu int) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		if _, ok := errors.AsType[netlink.LinkNotFoundError](err); !ok {
			return fmt.Errorf("lookup bridge %s: %w", name, err)
		}

		attrs := netlink.NewLinkAttrs()
		attrs.Name = name
		if mtu > 0 {
			attrs.MTU = mtu
		}
		bridge := &netlink.Bridge{LinkAttrs: attrs}
		if err := netlink.LinkAdd(bridge); err != nil {
			return fmt.Errorf("create bridge %s: %w", name, err)
		}
		link = bridge
	}

	if mtu > 0 && link.Attrs().MTU != mtu {
		if err := netlink.LinkSetMTU(link, mtu); err != nil {
			return fmt.Errorf("set bridge mtu %s: %w", name, err)
		}
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring bridge %s up: %w", name, err)
	}
	return nil
}

func (m *NetlinkManager) CreateBridgeAttachment(bridgeName, hostIfName, peerIfName string, peerPID, mtu int) error {
	if err := m.createVeth(hostIfName, peerIfName, mtu); err != nil {
		return err
	}

	hostLink, err := netlink.LinkByName(hostIfName)
	if err != nil {
		return fmt.Errorf("lookup host veth %s: %w", hostIfName, err)
	}
	peerLink, err := netlink.LinkByName(peerIfName)
	if err != nil {
		return fmt.Errorf("lookup peer veth %s: %w", peerIfName, err)
	}
	bridgeLink, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("lookup bridge %s: %w", bridgeName, err)
	}
	bridge, ok := bridgeLink.(*netlink.Bridge)
	if !ok {
		return fmt.Errorf("%s exists but is not a bridge", bridgeName)
	}

	if err := netlink.LinkSetMaster(hostLink, bridge); err != nil {
		return fmt.Errorf("attach %s to bridge %s: %w", hostIfName, bridgeName, err)
	}
	if err := netlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("bring host veth %s up: %w", hostIfName, err)
	}
	if err := netlink.LinkSetNsPid(peerLink, peerPID); err != nil {
		return fmt.Errorf("move %s to pid %d: %w", peerIfName, peerPID, err)
	}
	return nil
}

func (m *NetlinkManager) CreateP2PAttachment(aIfName string, aPID int, bIfName string, bPID int, mtu int) error {
	if err := m.createVeth(aIfName, bIfName, mtu); err != nil {
		return err
	}

	aLink, err := netlink.LinkByName(aIfName)
	if err != nil {
		return fmt.Errorf("lookup p2p veth %s: %w", aIfName, err)
	}
	bLink, err := netlink.LinkByName(bIfName)
	if err != nil {
		return fmt.Errorf("lookup p2p veth %s: %w", bIfName, err)
	}

	if err := netlink.LinkSetNsPid(aLink, aPID); err != nil {
		return fmt.Errorf("move %s to pid %d: %w", aIfName, aPID, err)
	}
	if err := netlink.LinkSetNsPid(bLink, bPID); err != nil {
		return fmt.Errorf("move %s to pid %d: %w", bIfName, bPID, err)
	}
	return nil
}

func (m *NetlinkManager) DeleteLink(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		if _, ok := errors.AsType[netlink.LinkNotFoundError](err); ok {
			return nil
		}
		return fmt.Errorf("lookup link %s: %w", name, err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete link %s: %w", name, err)
	}
	return nil
}

func (m *NetlinkManager) createVeth(name, peer string, mtu int) error {
	attrs := netlink.NewLinkAttrs()
	attrs.Name = name
	if mtu > 0 {
		attrs.MTU = mtu
	}

	veth := &netlink.Veth{
		LinkAttrs: attrs,
		PeerName:  peer,
	}
	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("create veth pair %s/%s: %w", name, peer, err)
	}
	return nil
}
