# Route Target Selective Import over VPNv4 RR

This example shows a shared-services L3VPN pattern built with plain VPNv4 route
targets. `red` and `blue` both import the shared-services RT, while `shared`
imports both tenant RTs. The route reflector stays policy-free and only
reflects labeled-unicast and VPNv4 reachability.

## Topology

- `rr`
  - Route reflector for transport and VPNv4
  - Loopback: `10.255.92.100/32`
- `red`
  - VRF `red`, host subnet `10.10.92.0/24`
  - Exports `RT 65000:100`
  - Imports `RT 65000:100` and `RT 65000:300`
- `blue`
  - VRF `blue`, host subnet `10.20.92.0/24`
  - Exports `RT 65000:200`
  - Imports `RT 65000:200` and `RT 65000:300`
- `shared`
  - VRF `shared`, host subnet `10.30.92.0/24`
  - Exports `RT 65000:300`
  - Imports `RT 65000:100` and `RT 65000:200`

## Reachability

`pings:` checks both tenant-to-service and service-to-tenant directions.

The useful thing to inspect after `up` is not just that pings work, but why:

```console
vtysh -c 'show bgp vrf red ipv4 unicast'
vtysh -c 'show bgp vrf blue ipv4 unicast'
vtysh -c 'show bgp vrf shared ipv4 unicast'
vtysh -c 'show bgp ipv4 vpn rd all'
```

Manual follow-up worth trying:

- `red` host to `blue` host should stay isolated.
- `blue` host to `red` host should stay isolated.
