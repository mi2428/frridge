# EVPN Type-2 over IP Clos

This example builds a 2-spine / 3-leaf IP Clos underlay and stretches one L2 segment across the three leaves with EVPN type-2 over VXLAN.
`lab.yaml` seeds the underlay, configures `sp1` and `sp2` as route reflectors, creates `br10` and `vxlan100` on each leaf, and places one `host` namespace behind each leaf on `10.10.10.0/24`.

## Topology

### Spine Routers

- `sp1`
  - Loopback: `10.255.0.1/32`
  - Role: IPv4 unicast / EVPN route reflector
  - Underlay links:
    - `eth1` to `lf1 eth1`: `10.0.0.1/31`
    - `eth2` to `lf2 eth1`: `10.0.0.5/31`
    - `eth3` to `lf3 eth1`: `10.0.0.9/31`
- `sp2`
  - Loopback: `10.255.0.2/32`
  - Role: IPv4 unicast / EVPN route reflector
  - Underlay links:
    - `eth1` to `lf1 eth2`: `10.0.0.3/31`
    - `eth2` to `lf2 eth2`: `10.0.0.7/31`
    - `eth3` to `lf3 eth2`: `10.0.0.11/31`

### Leaf Routers

- `lf1`
  - Loopback / VTEP: `10.255.0.11/32`
  - Underlay links:
    - `eth1` to `sp1 eth1`: `10.0.0.0/31`
    - `eth2` to `sp2 eth1`: `10.0.0.2/31`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.10.11/24`, MAC `02:00:00:00:10:11`
- `lf2`
  - Loopback / VTEP: `10.255.0.12/32`
  - Underlay links:
    - `eth1` to `sp1 eth2`: `10.0.0.4/31`
    - `eth2` to `sp2 eth2`: `10.0.0.6/31`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.10.12/24`, MAC `02:00:00:00:10:12`
- `lf3`
  - Loopback / VTEP: `10.255.0.13/32`
  - Underlay links:
    - `eth1` to `sp1 eth3`: `10.0.0.8/31`
    - `eth2` to `sp2 eth3`: `10.0.0.10/31`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.10.13/24`, MAC `02:00:00:00:10:13`

### Overlay Reachability

- EVPN type-2 advertises the host MAC/IP entries between `lf1`, `lf2`, and `lf3`.
- `pings:` in `lab.yaml` checks `lf1 -> lf2`, `lf1 -> lf3`, and `lf2 -> lf3` across the VXLAN segment.
