# EVPN Type-3 (1 RR, 2 VTEPs)

This example builds one route reflector and two VTEPs.
`lab.yaml` creates one L2VNI on each PE and enables `advertise-all-vni`, which is enough to originate EVPN inclusive multicast Ethernet tag routes for the shared VNI.

## Topology

### Route Reflector

- `rr`
  - Loopback: `10.255.83.100/32`
  - Role: IPv4 unicast / EVPN route reflector
  - Underlay links:
    - `eth1` to `pe1 eth1`: `10.0.83.1/31`
    - `eth2` to `pe2 eth1`: `10.0.83.3/31`

### VTEP 1

- `pe1`
  - Loopback / VTEP: `10.255.83.11/32`
  - Underlay link:
    - `eth1` to `rr eth1`: `10.0.83.0/31`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.83.11/24`

### VTEP 2

- `pe2`
  - Loopback / VTEP: `10.255.83.12/32`
  - Underlay link:
    - `eth1` to `rr eth2`: `10.0.83.2/31`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.83.12/24`

### Verification

- EVPN type-3 routes are IMET advertisements for a VNI.
- After `up`, inspect `show bgp l2vpn evpn route type 3` on `rr`, `pe1`, or `pe2`.
- In this example, the key control-plane signal is the VNI presence itself; no host traffic is required before the type-3 routes appear.
