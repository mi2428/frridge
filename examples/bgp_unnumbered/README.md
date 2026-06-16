# BGP Unnumbered (2 Routers)

This example builds a 2-router point-to-point link and runs eBGP unnumbered across it.
`lab.yaml` leaves the transport interface unnumbered, forms the session over IPv6 link-local addresses, and exchanges IPv4 loopbacks in the IPv4 unicast address family.

## Topology

### AS 65101 Router

- `r1`
  - Loopback: `10.255.72.1/32`
  - BGP unnumbered link:
    - `eth1` to `r2 eth1`

### AS 65102 Router

- `r2`
  - Loopback: `10.255.72.2/32`
  - BGP unnumbered link:
    - `eth1` to `r1 eth1`

### Reachability

- `pings:` in `lab.yaml` checks IPv4 reachability from `r1` to the `r2` loopback over the unnumbered BGP session.
- Right after `up`, give BGP a short moment to establish before treating the first ping result as final.
