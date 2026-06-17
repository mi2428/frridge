# BGP SRv6 uSID Dual-Stack L3VPN

This example combines the dual-stack service checks from
`srv6_l3vpn_end_dt4_dt6` with the uSID locator model from
`bgp_srv6_usid_l3vpn`.
Each PE uses a `/48` locator with `behavior usid` and `format usid-f3216`.
Inside each VRF, `sid vpn per-vrf export auto` asks Zebra to allocate one
shared uSID-flavored service SID for that tenant, then both IPv4 and IPv6 VPN
routes reuse the same per-VRF SRv6 endpoint.

## Topology

### Routers

- `r1`
  - Route reflector / transit PE in AS `65001`
  - uSID locator: `fcbb:0:1::/48`
  - VRFs:
    - `vrf10`
    - `vrf20`
- `r2`
  - Remote PE in AS `65002`
  - uSID locator: `fcbb:0:2::/48`
  - VRFs:
    - `vrf10`
    - `vrf20`
- `r3`
  - Remote PE in AS `65001`
  - uSID locator: `fcbb:0:3::/48`
  - VRFs:
    - `vrf10`
    - `vrf20`

### Reachability

- `pings:` checks both IPv4 and IPv6 customer reachability in two different
  VRFs.
- The interesting part is that the same per-VRF uSID service SID carries:
  - VPNv4 routes that resolve to `End.DT4`
  - VPNv6 routes that resolve to `End.DT6`
- Additional pings keep the IPv6 underlay loopbacks honest so the service
  routes are not mistaken for plain CE-side routing.

Useful checks:

- `show segment-routing srv6 locator`
- `show segment-routing srv6 sid`
- `show bgp ipv4 vpn`
- `show bgp ipv6 vpn`
- `show bgp vrf vrf10 ipv4 unicast`
- `show bgp vrf vrf10 ipv6 unicast`
