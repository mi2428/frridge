# IS-IS (3 Routers)

This example builds a 3-router line and runs IS-IS level-2 for IPv4 reachability.
`lab.yaml` puts one /32 loopback on each router, uses /31 point-to-point transport links, enables the IPv4 unicast topology with wide metrics, and redistributes connected IPv4 prefixes into IS-IS level-2.

## Topology

### Router 1

- `r1`
  - Loopback: `10.255.71.1/32`
  - IS-IS links:
    - `eth1` to `r2 eth1`: `10.0.71.0/31`

### Router 2

- `r2`
  - Loopback: `10.255.71.2/32`
  - IS-IS links:
    - `eth1` to `r1 eth1`: `10.0.71.1/31`
    - `eth2` to `r3 eth1`: `10.0.71.2/31`

### Router 3

- `r3`
  - Loopback: `10.255.71.3/32`
  - IS-IS links:
    - `eth1` to `r2 eth2`: `10.0.71.3/31`

### Reachability

- `pings:` in `lab.yaml` checks end-to-end IPv4 reachability from `r1` to the `r3` loopback.
- Right after `up`, give IS-IS around 20-30 seconds to converge before treating the first ping result as final.
