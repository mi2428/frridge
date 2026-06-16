# iBGP Route Reflector (3 Routers)

This example builds one route reflector and two iBGP clients.
`lab.yaml` uses numbered IPv4 transit links, installs only the static routes needed to reach the BGP session loopbacks, and relies on the route reflector to exchange the client host subnets.

## Topology

### Route Reflector

- `rr`
  - Loopback: `10.255.73.100/32`
  - iBGP clients:
    - `c1` via `eth1`: `192.0.2.1/31` to `c1 eth1 192.0.2.0/31`
    - `c2` via `eth2`: `192.0.2.2/31` to `c2 eth1 192.0.2.3/31`

### Client 1

- `c1`
  - Session loopback: `10.255.73.11/32`
  - Advertised host subnet: `10.10.73.0/24`
  - Local host namespace:
    - `host` on `br10`: router IP `10.10.73.1/24`, host IP `10.10.73.11/24`
  - iBGP peer:
    - `rr` over `eth1`

### Client 2

- `c2`
  - Session loopback: `10.255.73.12/32`
  - Advertised host subnet: `10.20.73.0/24`
  - Local host namespace:
    - `host` on `br20`: router IP `10.20.73.1/24`, host IP `10.20.73.12/24`
  - iBGP peer:
    - `rr` over `eth1`

### Reachability

- `c1` and `c2` do not peer with each other directly. They only form iBGP sessions with `rr`.
- `pings:` in `lab.yaml` checks whether the `c1` host namespace can reach the `c2` host `10.20.73.12` through reflected iBGP routes, with the probe source pinned to `10.10.73.11`.
- Right after `up`, give BGP a few seconds to establish the two client sessions and reflect the client routes before treating the first ping result as final.
