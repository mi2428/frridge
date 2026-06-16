# IS-IS Level-1-2 (3 Routers)

This example builds a 3-router line and runs IS-IS with `is-type level-1-2` on every router.
`lab.yaml` keeps all routers in one area, enables the IPv4 unicast topology with wide metrics, and redistributes connected IPv4 prefixes into both level-1 and level-2 so the control plane can be inspected from either level.

## Topology

### Router 1

- `r1`
  - Loopback: `10.255.73.1/32`
  - IS-IS links:
    - `eth1` to `r2 eth1`: `10.0.73.0/31`

### Router 2

- `r2`
  - Loopback: `10.255.73.2/32`
  - IS-IS links:
    - `eth1` to `r1 eth1`: `10.0.73.1/31`
    - `eth2` to `r3 eth1`: `10.0.73.2/31`

### Router 3

- `r3`
  - Loopback: `10.255.73.3/32`
  - IS-IS links:
    - `eth1` to `r2 eth2`: `10.0.73.3/31`

### Reachability

- `pings:` in `lab.yaml` checks end-to-end IPv4 reachability from `r1` to the `r3` loopback.
- This is the all-routers `level-1-2` version of the same line topology, useful for comparing the config and LSDB output against the `level1` and `level2` samples.
- Right after `up`, give IS-IS around 20-30 seconds to converge before treating the first ping result as final.
