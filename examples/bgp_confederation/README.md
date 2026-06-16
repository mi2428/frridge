# BGP Confederation

This lab splits provider AS `65000` into confederation member ASes `65001`
and `65002`, then peers the confederation edge with external AS `65100`.
It shows how the inside of the provider can use eBGP-like member-AS sessions
while the customer-facing edge still presents one public AS.

## Topology

- `r1`
  - Member AS: `65001`
  - Confederation identifier: `65000`
  - Host namespace: `10.10.101.11/24`
- `r2`
  - Member AS: `65002`
  - Confederation identifier: `65000`
  - External edge toward AS `65100`
- `r3`
  - External AS: `65100`
  - Host namespace: `10.30.101.11/24`

`pings:` checks host-to-host reachability through the confederation boundary.
