# eBGP Inter-AS Option C (2 Routers)

This example collapses each domain into one border router and models option C as loopback-to-loopback eBGP over a separate transport link.
`lab.yaml` uses `eth1` only for transport, `linux.routes` provides the loopback reachability, and the actual eBGP session is pinned to the loopbacks.

## Topology

### AS 65001 Border Router

- `as1`
  - Loopback / eBGP peer: `10.255.2.1/32`
  - Transport link:
    - `eth1` to `as2 eth1`: `192.0.2.0/31`
  - Local LAN:
    - `br10`: `10.10.30.1/24`
    - `host` netns: `10.10.30.11/24`, MAC `02:00:00:00:c0:11`

### AS 65002 Border Router

- `as2`
  - Loopback / eBGP peer: `10.255.2.2/32`
  - Transport link:
    - `eth1` to `as1 eth1`: `192.0.2.1/31`
  - Local LAN:
    - `br20`: `10.20.30.1/24`
    - `host` netns: `10.20.30.12/24`, MAC `02:00:00:00:c0:12`

### Inter-AS Behavior

- The border link only provides transport reachability between the loopbacks.
- Static host routes keep the loopback-based eBGP session simple in this two-router lab.
- The service routes are exchanged on the multihop loopback session rather than on the border interface addresses.

### Reachability

- `pings:` in `lab.yaml` checks routed reachability from the AS 65001 host to the AS 65002 host.
- Right after `up`, give BGP a short moment to converge before treating the first ping result as final.
