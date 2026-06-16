# BGP SRv6 SID Explicit

This example uses the same two-VRF SRv6 L3VPN topology as
`bgp_srv6_l3vpn`, but pins each VRF to an explicit SRv6 service SID
instead of letting FRR derive the SID from a numeric export index.

## Topology

### Routers

- `r1`
  - Locator: `2001:db8:1:1::/64`
  - Explicit service SIDs:
    - `vrf10 = 2001:db8:1:1:1000::`
    - `vrf20 = 2001:db8:1:1:2000::`
- `r2`
  - Locator: `2001:db8:2:2::/64`
  - Explicit service SIDs:
    - `vrf10 = 2001:db8:2:2:1000::`
    - `vrf20 = 2001:db8:2:2:2000::`
- `r3`
  - Locator: `2001:db8:3:3::/64`
  - Explicit service SIDs:
    - `vrf10 = 2001:db8:3:3:1000::`
    - `vrf20 = 2001:db8:3:3:2000::`

### Reachability

- `pings:` checks the underlay/locator reachability that the explicit service
  routes depend on.
- The interesting inspection points are:
  - `show segment-routing srv6 sid`
  - `show bgp ipv4 vpn`
  - `show bgp vrf vrf10 ipv4 unicast`
