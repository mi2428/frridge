# EVPN PIM BUM over IPv6 Clos

This example keeps EVPN for MAC route exchange but sends BUM traffic over an
IPv6 multicast underlay.
The fabric is a 2-spine / 3-leaf Clos with an IPv6-only routed underlay.
To keep the Linux VXLAN dataplane deterministic in a small lab, each VXLAN
device is pinned to `eth1` toward `sp1`, while the full dual-spine underlay
still carries the EVPN control plane and IPv6 loopback reachability.

## Topology

### Spine Routers

- `sp1`
  - IPv6 loopback: `2001:db8:176::1/128`
  - Role: IPv6 underlay / EVPN route reflector / PIMv6 RP
- `sp2`
  - IPv6 loopback: `2001:db8:176::2/128`
  - Role: IPv6 underlay / EVPN route reflector / PIMv6 transit

### Leaf Routers

- `lf1`
  - IPv6 loopback / VTEP: `2001:db8:176::11/128`
  - Host namespace: `10.10.176.11/24`
- `lf2`
  - IPv6 loopback / VTEP: `2001:db8:176::12/128`
  - Host namespace: `10.10.176.12/24`
- `lf3`
  - IPv6 loopback / VTEP: `2001:db8:176::13/128`
  - Host namespace: `10.10.176.13/24`

## Reachability

- EVPN distributes the host MAC/IP bindings across the Clos.
- VXLAN BUM replication uses IPv6 multicast group `ff3e::176` through PIMv6.
- `pings:` checks:
  - `lf1 -> lf2`
  - `lf1 -> lf3`
  - `lf2 -> lf3`
