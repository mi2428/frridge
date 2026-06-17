# EVPN Route Types 1-5 with Anycast Gateway over 2x3 Clos

This example combines EVPN multihoming with symmetric IRB on a 2-spine / 3-leaf
IPv4 Clos fabric.
`lf1` and `lf2` multihome one access host into the same Ethernet Segment,
`lf3` provides a remote single-homed access port in the same stretched subnet,
and each leaf also owns one routed services subnet exported through a shared
tenant L3VNI.

## Topology

### Spine Routers

- `sp1`
  - Loopback: `10.255.97.1/32`
  - Role: IPv4 underlay / EVPN route reflector
- `sp2`
  - Loopback: `10.255.97.2/32`
  - Role: IPv4 underlay / EVPN route reflector

### Leaf Routers

- `lf1`
  - Loopback / VTEP: `10.255.97.11/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Shared access segment:
    - Anycast gateway: `10.10.40.1/24`
    - Anycast MAC: `02:00:00:10:40:01`
    - L2VNI: `100`
  - EVPN multihoming:
    - `bond0`
    - ESI `1`
    - ES system MAC `02:00:00:00:97:aa`
    - DF preference `200`
  - Local services subnet:
    - `10.10.51.0/24`
- `lf2`
  - Loopback / VTEP: `10.255.97.12/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Shared access segment:
    - Anycast gateway: `10.10.40.1/24`
    - Anycast MAC: `02:00:00:10:40:01`
    - L2VNI: `100`
  - EVPN multihoming:
    - `bond0`
    - ESI `1`
    - ES system MAC `02:00:00:00:97:aa`
    - DF preference `100`
  - Local services subnet:
    - `10.10.52.0/24`
- `lf3`
  - Loopback / VTEP: `10.255.97.13/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Shared access segment:
    - Anycast gateway: `10.10.40.1/24`
    - Anycast MAC: `02:00:00:10:40:01`
    - L2VNI: `100`
  - Local services subnet:
    - `10.10.53.0/24`

### Endpoints

- `srv`
  - Dual-homed host on the shared access subnet
  - `bond0`: `10.10.40.11/24`
- `host31`
  - Single-homed remote access host behind `lf3`
  - `10.10.40.31/24`
- `svc11`
  - Local services host behind `lf1`
  - `10.10.51.11/24`
- `svc21`
  - Local services host behind `lf2`
  - `10.10.52.11/24`
- `svc31`
  - Local services host behind `lf3`
  - `10.10.53.11/24`

## Route Types in This Lab

- Type-1: `lf1` and `lf2` advertise EAD-per-ES / EAD-per-EVI state for the
  multihomed access segment.
- Type-2: the shared-subnet endpoints (`srv` and `host31`) are learned and
  exchanged as MAC/IP routes.
- Type-3: each leaf advertises the shared L2VNI `100` with `advertise-all-vni`.
- Type-4: `lf1` and `lf2` advertise Ethernet Segment routes for the shared ESI.
- Type-5: each leaf exports only its local services subnet into the tenant VRF.

## Reachability

- `srv -> host31` stays inside the stretched L2 segment and depends on the
  multihomed access path plus EVPN type-2 learning.
- `srv -> svc31` leaves the access subnet through the anycast gateway and
  follows EVPN type-5 routing to `lf3`.
- `host31 -> svc11` exercises the same anycast-gateway behavior toward `lf1`.
- `svc11 -> svc21` verifies routed reachability between tenant prefixes across
  the shared L3VNI.

For compactness, this lab keeps the anycast IP/MAC directly on `br10` rather
than using a helper macvlan device.
It also seeds a permanent neighbor entry for the dual-homed host on `lf1` and
`lf2` so the multihomed endpoint shows up as an EVPN type-2 MAC/IP route
without depending on extra ARP warm-up timing during `up`.

Useful checks:

- `show bgp l2vpn evpn route type 1`
- `show bgp l2vpn evpn route type 2`
- `show bgp l2vpn evpn route type 3`
- `show bgp l2vpn evpn route type 4`
- `show bgp l2vpn evpn route type 5`
- `show bgp vrf tenant ipv4 unicast`
- `show evpn vni detail`
