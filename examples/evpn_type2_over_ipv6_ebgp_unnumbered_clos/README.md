# EVPN Type-2 over IPv6 eBGP Unnumbered Clos

This example builds a 2-spine / 3-leaf Clos where every leaf-to-spine session
is eBGP unnumbered over IPv6 link-local addresses.
Each leaf advertises one IPv6 VTEP loopback in `address-family ipv6 unicast`
and stretches one shared L2 segment across the fabric with EVPN type-2 over
VXLAN.

## Topology

### Spine Routers

- `sp1`
  - ASN: `65101`
  - IPv6 loopback / VTEP reachability source: `2001:db8:171::1/128`
  - Role: eBGP transit for IPv6 underlay and EVPN
- `sp2`
  - ASN: `65102`
  - IPv6 loopback / VTEP reachability source: `2001:db8:171::2/128`
  - Role: eBGP transit for IPv6 underlay and EVPN

### Leaf Routers

- `lf1`
  - ASN: `65111`
  - IPv6 loopback / VTEP: `2001:db8:171::11/128`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.171.11/24`, MAC `02:00:00:00:71:11`
- `lf2`
  - ASN: `65112`
  - IPv6 loopback / VTEP: `2001:db8:171::12/128`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.171.12/24`, MAC `02:00:00:00:71:12`
- `lf3`
  - ASN: `65113`
  - IPv6 loopback / VTEP: `2001:db8:171::13/128`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.171.13/24`, MAC `02:00:00:00:71:13`

## Reachability

- The fabric uses only IPv6 transport on the underlay links.
- Every spine and leaf peers with `neighbor ethX interface remote-as external`,
  so the control plane runs on IPv6 link-local adjacency rather than numbered
  point-to-point addresses.
- `pings:` checks host-to-host reachability across the EVPN VXLAN segment:
  - `lf1 -> lf2`
  - `lf1 -> lf3`
  - `lf2 -> lf3`
