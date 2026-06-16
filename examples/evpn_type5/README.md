# EVPN Type-5 (1 RR, 2 PEs)

This example builds one route reflector and two PEs, each with one local tenant subnet.
`lab.yaml` uses an EVPN underlay in the default VRF, then creates a tenant IP-VRF with L3VNI `5000` on each PE and exports the local connected subnet into EVPN as type-5 routes.

This example requires a Linux kernel that supports `ip link add ... type vrf`.
If the host kernel lacks VRF netdevice support, `up` will fail during the router-local shell bootstrap before FRR starts importing or exporting type-5 routes.

## Topology

### Route Reflector

- `rr`
  - Loopback: `10.255.85.100/32`
  - Role: IPv4 unicast / EVPN route reflector
  - Underlay links:
    - `eth1` to `pe1 eth1`: `10.0.85.1/31`
    - `eth2` to `pe2 eth1`: `10.0.85.3/31`

### PE 1

- `pe1`
  - Loopback / VTEP: `10.255.85.11/32`
  - Underlay link:
    - `eth1` to `rr eth1`: `10.0.85.0/31`
  - Tenant VRF:
    - `vrf tenant`
    - L3VNI: `5000`
    - Local subnet: `10.10.85.0/24`
    - Local host netns: `10.10.85.11/24`

### PE 2

- `pe2`
  - Loopback / VTEP: `10.255.85.12/32`
  - Underlay link:
    - `eth1` to `rr eth2`: `10.0.85.2/31`
  - Tenant VRF:
    - `vrf tenant`
    - L3VNI: `5000`
    - Local subnet: `10.20.85.0/24`
    - Local host netns: `10.20.85.11/24`

### Reachability

- Each PE exports its connected tenant subnet into EVPN with `advertise ipv4 unicast`, which originates EVPN type-5 routes.
- `pings:` in `lab.yaml` checks whether the `pe1` tenant host can reach the `pe2` tenant host through the L3VNI.
