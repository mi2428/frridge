//go:build linux

package netlink

import vnetlink "github.com/vishvananda/netlink"

func handleAddrReplace(handle *vnetlink.Handle, link vnetlink.Link, addr *vnetlink.Addr) error {
	return handle.AddrReplace(link, addr)
}

func handleLinkSetBrNeighSuppress(handle *vnetlink.Handle, link vnetlink.Link, mode bool) error {
	return handle.LinkSetBrNeighSuppress(link, mode)
}
