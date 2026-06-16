# SRv6 uSID L3VPN

This example carries two IPv4 VRFs across an IPv6 underlay with BGP VPNv4 and
SRv6 uSID service SIDs. Each PE uses an SRv6 locator with `behavior usid` and
`format usid-f3216`, so BGP service SIDs are allocated from a compressed uSID
locator instead of a classic `/64` SRv6 locator.

## Topology

### Routers

- `r1`
  - Route reflector / transit PE in AS `65001`
  - Locator: `fc00:0:1::/48` with `behavior usid` and `format usid-f3216`
  - VRFs:
    - `vrf10` on `eth3`
    - `vrf20` on `eth4`
- `r2`
  - Remote PE in AS `65002`
  - Locator: `fc00:0:2::/48` with `behavior usid` and `format usid-f3216`
  - VRFs:
    - `vrf10` on `eth2`
    - `vrf20` on `eth3`
- `r3`
  - Remote PE in AS `65001`
  - Locator: `fc00:0:3::/48` with `behavior usid` and `format usid-f3216`
  - VRFs:
    - `vrf10` on `eth2`
    - `vrf20` on `eth3`

### Reachability

- `pings:` checks the underlay/locator reachability that the SRv6 service
  routes depend on.
- The interesting state is in the control-plane and VRF RIBs:
  - `show bgp ipv4 vpn`
  - `show bgp vrf vrf10 ipv4 unicast`
  - `show ip route vrf vrf10 192.168.3.0/24 json`
  - `show segment-routing srv6 sid`
  - `show segment-routing srv6 locator`
