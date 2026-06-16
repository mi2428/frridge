# EVPN Virtual LAN

This example shows the EVPN "virtual LAN" service model that FRR implements as
a MAC-VRF with a VLAN-based service interface.

## Topology

### Route Reflector

- `rr`
  - Loopback: `10.255.80.100/32`
  - Role: IPv4 underlay route reflector and EVPN route reflector

### VTEP 1

- `pe1`
  - Loopback / VTEP: `10.255.80.11/32`
  - VLAN / MAC-VRF:
    - `br10`
    - `vxlan100` with VNI `100`
    - Local host namespace: `10.10.80.11/24`

### VTEP 2

- `pe2`
  - Loopback / VTEP: `10.255.80.12/32`
  - VLAN / MAC-VRF:
    - `br10`
    - `vxlan100` with VNI `100`
    - Local host namespace: `10.10.80.12/24`

### Reachability

- This is a VLAN-based EVPN service: one L2VNI, one stretched MAC-VRF, and
  two hosts in the same subnet.
- `pings:` checks `pe1` host namespace to `pe2` host namespace across VXLAN.
- Useful follow-up commands:
  - `show evpn vni`
  - `show bgp l2vpn evpn route type 2`
  - `bridge fdb show`
