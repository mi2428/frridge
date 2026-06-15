# eBGP Inter-AS Option B (2 Routers)

This example collapses each domain into one border router and models option B as direct eBGP exchange across the inter-AS link.
`lab.yaml` uses `eth1` for the border peering and carries each side's local LAN prefix directly in IPv4 unicast BGP.

## Topology

### AS 65001 Border Router

- `as1`
  - Loopback: `10.255.1.1/32`
  - Border link:
    - `eth1` to `as2 eth1`: `192.0.2.0/31`
  - Local LAN:
    - `br10`: `10.10.20.1/24`
    - `host` netns: `10.10.20.11/24`, MAC `02:00:00:00:b0:11`

### AS 65002 Border Router

- `as2`
  - Loopback: `10.255.1.2/32`
  - Border link:
    - `eth1` to `as1 eth1`: `192.0.2.1/31`
  - Local LAN:
    - `br20`: `10.20.20.1/24`
    - `host` netns: `10.20.20.12/24`, MAC `02:00:00:00:b0:12`

### Inter-AS Behavior

- The ASBRs peer directly on `eth1`.
- Each side advertises its loopback and local LAN prefix to the other side.
- The service routes are exchanged on the same direct border session.

### Reachability

- `pings:` in `lab.yaml` checks routed reachability from the AS 65001 host to the AS 65002 host.
