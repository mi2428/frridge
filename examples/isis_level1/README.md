# IS-IS Level-1 (3 Routers)

This example builds a 3-router line and runs IS-IS level-1 for IPv4 reachability inside one area.
`lab.yaml` keeps all three routers in area `49.0001`, uses /31 point-to-point transport links, enables the IPv4 unicast topology with wide metrics, and redistributes connected IPv4 prefixes into IS-IS level-1.

## Topology

### Router 1

- `r1`
  - Loopback: `10.255.72.1/32`
  - IS-IS links:
    - `eth1` to `r2 eth1`: `10.0.72.0/31`

### Router 2

- `r2`
  - Loopback: `10.255.72.2/32`
  - IS-IS links:
    - `eth1` to `r1 eth1`: `10.0.72.1/31`
    - `eth2` to `r3 eth1`: `10.0.72.2/31`

### Router 3

- `r3`
  - Loopback: `10.255.72.3/32`
  - IS-IS links:
    - `eth1` to `r2 eth2`: `10.0.72.3/31`

### Reachability

- `pings:` in `lab.yaml` checks end-to-end IPv4 reachability from `r1` to the `r3` loopback.
- This is the single-area L1 version of the line topology, so every adjacency and redistributed prefix lives in level-1.
- Right after `up`, give IS-IS around 20-30 seconds to converge before treating the first ping result as final.
