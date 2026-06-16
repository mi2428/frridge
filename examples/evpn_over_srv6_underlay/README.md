# EVPN over SRv6 Underlay (1 RR, 2 VTEPs)

This example builds one route reflector and two VTEPs.
`lab.yaml` uses IPv4 loopbacks and point-to-point underlay links for BGP reachability, then stretches a single L2 segment with EVPN over an SRv6-capable IS-IS underlay over VXLAN.

## Topology

### Route Reflector

- `rr`
  - Loopback: `10.255.80.100/32`
  - Role: IPv4 unicast / EVPN route reflector
  - Underlay links:
    - `eth1` to `pe1 eth1`: `10.0.80.1/31`
    - `eth2` to `pe2 eth1`: `10.0.80.3/31`

### VTEP 1

- `pe1`
  - Loopback / VTEP: `10.255.80.11/32`
  - Underlay link:
    - `eth1` to `rr eth1`: `10.0.80.0/31`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.80.11/24`, MAC `02:00:00:00:80:11`

### VTEP 2

- `pe2`
  - Loopback / VTEP: `10.255.80.12/32`
  - Underlay link:
    - `eth1` to `rr eth2`: `10.0.80.2/31`
  - Overlay:
    - `br10`
    - `vxlan100` with VNI `100`
    - `host` netns: `10.10.80.12/24`, MAC `02:00:00:00:80:12`

### Reachability

- EVPN over an SRv6-capable IS-IS underlay advertises the local MAC/IP bindings for the two host namespaces.
- `pings:` in `lab.yaml` checks `pe1` host namespace to `pe2` host namespace across the VXLAN segment.
