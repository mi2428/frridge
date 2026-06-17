# iBGP Route Reflector Add-Path

This example builds one route reflector, one ingress router, and two exit
routers. Both exits advertise the same service prefix, and the route reflector
uses `addpath-tx-all-paths` so the ingress router can receive both reflected
paths for that one prefix.

## Topology

- `ingress`
  - Loopback: `10.255.73.11/32`
  - Local host namespace: `10.10.73.11/24`
  - Receives up to eight reflected paths from `rr` with
    `addpath-rx-paths-limit 8`
  - Static routes point remote client loopbacks at `rr` so reflected next-hops
    stay resolvable without an IGP
- `rr`
  - Loopback: `10.255.73.100/32`
  - Route reflector for `ingress`, `exit1`, and `exit2`
  - Sends all available paths for the same prefix to `ingress`
- `exit1` and `exit2`
  - Loopbacks: `10.255.73.21/32`, `10.255.73.22/32`
  - Both advertise the same service prefix `10.30.73.0/24`
  - Each keeps a static route back to `ingress` loopback through `rr`
- `service`
  - Service endpoint: `10.30.73.33/24`

## Reachability

`pings:` checks the ingress host reaching the shared service endpoint.

The interesting part is the control plane on `ingress`:

```console
vtysh -c 'show bgp ipv4 unicast 10.30.73.0/24'
vtysh -c 'show bgp neighbors 10.255.73.100 advertised-routes'
```

After `up`, `ingress` should have two BGP paths for `10.30.73.0/24`, both
reflected by `rr` from `exit1` and `exit2`.
