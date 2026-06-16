# BGP PIC Edge Failover

This lab builds the BGP state that an edge router needs for PIC-style failover:
the edge receives two BGP paths for the same service prefix through a route
reflector, negotiates add-path from the RR, and installs up to two iBGP paths.

## Topology

- `edge`
  - Local host namespace: `10.10.74.11/24`
  - iBGP session to `rr`
  - `maximum-paths ibgp 2`
- `rr`
  - Route reflector for `edge`, `exit1`, and `exit2`
  - Sends all paths to `edge` with `addpath-tx-all-paths`
- `exit1` and `exit2`
  - Both advertise `10.30.74.0/24`
  - Both attach to the service LAN
- `service`
  - Service endpoint: `10.30.74.33/24`

`pings:` checks the edge host reaching the service endpoint while the edge has
backup BGP path state for the service prefix.
