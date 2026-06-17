# Seamless MPLS Multi-Domain

This example builds a five-router BGP labeled-unicast chain across three
autonomous-system domains.
`lab.yaml` keeps the control plane simple: both edge PEs originate one local
access subnet plus one loopback, and the intermediate ABR/core routers only
exchange `ipv4 labeled-unicast` toward their adjacent domains.

## Topology

### Routers

- `pe1`
  - AS `65001`
  - Loopback: `10.255.84.11/32`
  - Local host namespace: `10.10.84.11/24`
- `abr1`
  - AS `65001`
  - Left-domain ABR toward `core`
  - Loopback: `10.255.84.1/32`
- `core`
  - AS `65002`
  - Transit labeled-unicast speaker between the two edge domains
  - Loopback: `10.255.84.2/32`
- `abr2`
  - AS `65003`
  - Right-domain ABR toward `core`
  - Loopback: `10.255.84.3/32`
- `pe2`
  - AS `65003`
  - Loopback: `10.255.84.12/32`
  - Local host namespace: `10.20.84.12/24`

### Reachability

- `pings:` checks `pe1` host `10.10.84.11` reaching `pe2` host `10.20.84.12`
  across the labeled-unicast domains.
- Useful follow-up commands:
  - `show bgp ipv4 labeled-unicast`
  - `show mpls table`
- The interesting part of this lab is the label handoff between
  `pe1 -> abr1 -> core -> abr2 -> pe2` rather than any IGP or SR policy.
