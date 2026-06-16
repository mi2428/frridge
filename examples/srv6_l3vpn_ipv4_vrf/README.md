# SRv6 L3VPN IPv4 VRF

This example carries two IPv4 VRFs across an IPv6 underlay with BGP VPNv4 and
SRv6 service SIDs.

## Topology

### Routers

- `r1`
  - Route reflector / transit PE in AS `65001`
  - Locator: `2001:db8:1:1::/64`
  - VRFs:
    - `vrf10` on `eth3`
    - `vrf20` on `eth4`
- `r2`
  - Remote PE in AS `65002`
  - Locator: `2001:db8:2:2::/64`
  - VRFs:
    - `vrf10` on `eth2`
    - `vrf20` on `eth3`
- `r3`
  - Remote PE in AS `65001`
  - Locator: `2001:db8:3:3::/64`
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
