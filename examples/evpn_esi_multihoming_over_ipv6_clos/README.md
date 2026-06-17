# EVPN ESI Multihoming over IPv6 Clos

This example stretches one L2 segment across a 2-spine / 3-leaf Clos with an
IPv6-only underlay.
`lf1` and `lf2` advertise the same Ethernet Segment into EVPN and dual-home a
single CE server, while `lf3` acts as the remote VTEP for failover and
reachability checks.

## Topology

### Spine Routers

- `sp1`
  - IPv6 loopback: `2001:db8:181::1/128`
  - Role: IPv6 underlay / EVPN route reflector
- `sp2`
  - IPv6 loopback: `2001:db8:181::2/128`
  - Role: IPv6 underlay / EVPN route reflector

### Leaf Routers

- `lf1`
  - IPv6 loopback / VTEP: `2001:db8:181::11/128`
  - EVPN multihoming:
    - `bond0`
    - ESI `1`
    - ES system MAC `02:00:00:00:81:aa`
    - DF preference `200`
- `lf2`
  - IPv6 loopback / VTEP: `2001:db8:181::12/128`
  - EVPN multihoming:
    - `bond0`
    - ESI `1`
    - ES system MAC `02:00:00:00:81:aa`
    - DF preference `100`
- `lf3`
  - IPv6 loopback / VTEP: `2001:db8:181::13/128`
  - Remote single-homed access port toward `host31`

### Attached Hosts

- `srv`
  - CE-side `bond0`: `10.10.181.11/24`
  - Dual-homed across `lf1` and `lf2`
- `host31`
  - Single-homed behind `lf3`
  - `10.10.181.13/24`

## Reachability

- `lf1` and `lf2` originate EVPN type-1/type-4 state for the shared ESI.
- `lf3` learns the multihomed CE MAC/IP through EVPN and reaches it through the
  Clos fabric.
- `pings:` checks:
  - `srv -> host31`
  - `host31 -> srv`
