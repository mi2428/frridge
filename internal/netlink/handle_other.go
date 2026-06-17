//go:build !linux

package netlink

import vnetlink "github.com/vishvananda/netlink"

func handleAddrReplace(_ *vnetlink.Handle, _ vnetlink.Link, _ *vnetlink.Addr) error {
	return vnetlink.ErrNotImplemented
}

func handleLinkSetBrNeighSuppress(_ *vnetlink.Handle, _ vnetlink.Link, _ bool) error {
	return vnetlink.ErrNotImplemented
}
