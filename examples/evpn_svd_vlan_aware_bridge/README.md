# EVPN SVD VLAN-Aware Bridge

This example shows a single VXLAN device (SVD) per PE with a VLAN-aware Linux
bridge. One `vxlan0` carries two VLAN-backed L2VNIs instead of creating one
VXLAN device per VNI.

## Topology

- `rr`
  - Route reflector
- `pe1`
  - VTEP `10.255.82.11`
  - Access VLAN 10 host `10.10.82.11/24`
  - Access VLAN 20 host `10.20.82.11/24`
- `pe2`
  - VTEP `10.255.82.12`
  - Access VLAN 10 host `10.10.82.12/24`
  - Access VLAN 20 host `10.20.82.12/24`

Each PE creates:

- one VLAN-aware bridge `br0`
- one `external vnifilter` VXLAN device `vxlan0`
- VLAN to VNI mappings `10 -> 110` and `20 -> 220`
- one access port namespace per VLAN

## Reachability

`pings:` verifies that both VLANs stretch end-to-end across EVPN while sharing
the same VXLAN interface.

Useful checks:

- `bridge vlan tunnelshow`
- `bridge vni show dev vxlan0`
- `show evpn access-vlan detail`
- `show bgp l2vpn evpn route type 3`
