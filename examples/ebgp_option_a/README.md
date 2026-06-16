# eBGP Inter-AS Option A (4 Routers)

This example splits the topology into `PE -> ASBR -> ASBR -> PE`.
`lab.yaml` models option A as a per-service handoff: the tenant segment is carried across dedicated L2 links through the two ASBRs, while the ASBRs also keep a separate plain eBGP border session on `eth3`.

## Topology

### AS 65001

- `pe1`
  - Service handoff:
    - `eth1` to `asbr1 eth1`
  - Local attachment:
    - `host` netns: `10.10.10.11/24`, MAC `02:00:00:00:a0:11`
- `asbr1`
  - Loopback: `10.255.0.1/32`
  - Service handoff:
    - `eth1` to `pe1 eth1`
    - `eth2` to `asbr2 eth1`
  - Border link:
    - `eth3` to `asbr2 eth3`: `192.0.2.0/31`

### AS 65002

- `asbr2`
  - Loopback: `10.255.0.2/32`
  - Service handoff:
    - `eth1` to `asbr1 eth2`
    - `eth2` to `pe2 eth1`
  - Border link:
    - `eth3` to `asbr1 eth3`: `192.0.2.1/31`
- `pe2`
  - Service handoff:
    - `eth1` to `asbr2 eth2`
  - Local attachment:
    - `host` netns: `10.10.10.12/24`, MAC `02:00:00:00:a0:12`

### Inter-AS Behavior

- The ASBRs form a plain eBGP session on `eth3`.
- The tenant segment itself is handed off directly across the service-side L2 links and does not depend on BGP route exchange.
- This is the role-split learning version of per-service border handoff. It is intentionally simpler than a full RFC 4364 option-A deployment.

### Reachability

- `pings:` in `lab.yaml` checks `pe1` host to `pe2` host across the handed-off tenant segment.
