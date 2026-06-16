# OSPFv3 Over Link-Local Underlay (3 Routers)

This example builds a 3-router line and runs OSPFv3 for IPv6 reachability.
`lab.yaml` keeps the transit underlay on explicit IPv6 link-local addresses, marks each transit link as point-to-point, enables IPv6 forwarding, and advertises one IPv6 loopback from each router.

## Topology

### Router 1

- `r1`
  - Loopback: `2001:db8:110::1/128`
  - OSPFv3 links:
    - `eth1` to `r2 eth1`: `fe80::12:1`

### Router 2

- `r2`
  - Loopback: `2001:db8:110::2/128`
  - OSPFv3 links:
    - `eth1` to `r1 eth1`: `fe80::12:2`
    - `eth2` to `r3 eth1`: `fe80::23:1`

### Router 3

- `r3`
  - Loopback: `2001:db8:110::3/128`
  - OSPFv3 links:
    - `eth1` to `r2 eth2`: `fe80::23:2`

### Reachability

- `pings:` in `lab.yaml` checks end-to-end IPv6 reachability from `r1` to the `r3` loopback.
- Right after `up`, give OSPFv3 around 20-30 seconds to finish adjacency formation and install routes before treating the first ping result as final.
