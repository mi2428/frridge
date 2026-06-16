# EVPN ESI Multihoming Failover (1 RR, 2 Multihomed PEs)

This example builds one route reflector and two PEs that advertise the same Ethernet Segment.
`lab.yaml` creates `bond0` on each PE, assigns the same EVPN multihoming ESI to both bonds, attaches them to a local bridge/VXLAN pair for VNI `100`, and forms EVPN sessions to a single route reflector.

## Topology

### Route Reflector

- `rr`
  - Loopback: `10.255.81.100/32`
  - Role: IPv4 unicast / EVPN route reflector
  - Underlay links:
    - `eth1` to `pe1 eth1`: `10.0.81.1/31`
    - `eth2` to `pe2 eth1`: `10.0.81.3/31`

### PE 1

- `pe1`
  - Loopback / VTEP: `10.255.81.11/32`
  - Underlay link:
    - `eth1` to `rr eth1`: `10.0.81.0/31`
  - EVPN multihoming:
    - `bond0` built from `eth2`
    - Ethernet Segment ID derived from `es-id 1` and `es-sys-mac 02:00:00:00:81:aa`
    - DF preference: `200`

### PE 2

- `pe2`
  - Loopback / VTEP: `10.255.81.12/32`
  - Underlay link:
    - `eth1` to `rr eth2`: `10.0.81.2/31`
  - EVPN multihoming:
    - `bond0` built from `eth2`
    - Ethernet Segment ID derived from `es-id 1` and `es-sys-mac 02:00:00:00:81:aa`
    - DF preference: `100`

### Attached Segment

- `srv`
  - A simple attachment point on the shared `access` bridge
  - No routing config; it only keeps the access segment present and up

### Verification

- EVPN type-1/type-4 routes are EAD-per-ES / EAD-per-EVI advertisements for the shared Ethernet Segment.
- After `up`, inspect `show bgp l2vpn evpn route type 1` on `rr`, `pe1`, or `pe2` to confirm that the two PEs advertise the same Ethernet Segment into the EVPN control plane.
