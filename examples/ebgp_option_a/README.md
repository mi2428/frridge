# eBGP Inter-AS Option A (2 Routers)

This example collapses each domain into one border router and models option A as a per-service handoff.
`lab.yaml` keeps a plain eBGP session on `eth1`, while `linux.bridges` stitches the tenant segment through a dedicated interconnect on `eth2`.

## Topology

### AS 65001 Border Router

- `as1`
  - Loopback: `10.255.0.1/32`
  - Border link:
    - `eth1` to `as2 eth1`: `192.0.2.0/31`
  - Service handoff:
    - `eth2` is bridged into local `br10`
  - Local attachment:
    - `host` netns: `10.10.10.11/24`, MAC `02:00:00:00:a0:11`

### AS 65002 Border Router

- `as2`
  - Loopback: `10.255.0.2/32`
  - Border link:
    - `eth1` to `as1 eth1`: `192.0.2.1/31`
  - Service handoff:
    - `eth2` is bridged into local `br10`
  - Local attachment:
    - `host` netns: `10.10.10.12/24`, MAC `02:00:00:00:a0:12`

### Inter-AS Behavior

- The ASBRs form a plain eBGP session on `eth1`.
- The actual tenant segment is handed off directly on `eth2`, which `frridge` auto-attaches to `br10`.
- This is the collapsed two-router version of "per-service border interconnect".

### Reachability

- `pings:` in `lab.yaml` checks `as1 -> as2` across the bridged tenant segment.
