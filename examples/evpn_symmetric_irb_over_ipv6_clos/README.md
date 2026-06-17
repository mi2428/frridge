# EVPN Symmetric IRB over IPv6 Clos

This example turns the 2-spine / 3-leaf Clos underlay into a small symmetric
IRB fabric.
The underlay runs only on IPv6 point-to-point links, the spines act as EVPN
route reflectors, one student VLAN is stretched across all three leaves as an
L2VNI, and each leaf exports one local services subnet as EVPN type-5.

## Topology

### Spine Routers

- `sp1`
  - IPv6 loopback: `2001:db8:195::1/128`
  - Role: IPv6 underlay / EVPN route reflector
- `sp2`
  - IPv6 loopback: `2001:db8:195::2/128`
  - Role: IPv6 underlay / EVPN route reflector

### Leaf Routers

- `lf1`
  - IPv6 loopback / VTEP: `2001:db8:195::11/128`
  - Student bridge:
    - `br10`
    - Anycast gateway: `10.10.10.1/24`, MAC `02:00:00:10:10:01`
    - Attached host: `student11`
  - Local services subnet:
    - `10.10.20.0/24`
    - Attached host: `svc11`
- `lf2`
  - IPv6 loopback / VTEP: `2001:db8:195::12/128`
  - Student bridge:
    - `br10`
    - Anycast gateway: `10.10.10.1/24`, MAC `02:00:00:10:10:01`
    - Attached host: `student21`
  - Local services subnet:
    - `10.10.30.0/24`
    - Attached host: `svc21`
- `lf3`
  - IPv6 loopback / VTEP: `2001:db8:195::13/128`
  - Student bridge:
    - `br10`
    - Anycast gateway: `10.10.10.1/24`, MAC `02:00:00:10:10:01`
    - Attached host: `student31`
  - Local services subnet:
    - `10.10.40.0/24`
    - Attached host: `svc31`

## Reachability

- The stretched student subnet stays in EVPN type-2.
- Each leaf exports only its local services subnet as EVPN type-5.
- `pings:` checks:
  - `student11 -> student31`
  - `student11 -> svc21`
  - `svc31 -> svc11`
