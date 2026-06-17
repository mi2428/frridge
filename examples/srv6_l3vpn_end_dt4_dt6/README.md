# SRv6 L3VPN End.DT4/End.DT6

This example carries two dual-stack VRFs across an IPv6 underlay with BGP
VPNv4/VPNv6 and SRv6 service SIDs.
Each VRF exports both IPv4 and IPv6 connected routes, so the remote PE installs
matching `End.DT4` and `End.DT6` service SIDs for the same tenant.

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

- `pings:` checks both tenant VRFs reaching remote IPv4 and IPv6 service
  prefixes, plus the underlay locator reachability that those service routes
  depend on.
- The interesting state is in the control-plane and VRF RIBs:
  - `show bgp ipv4 vpn`
  - `show bgp ipv6 vpn`
  - `show bgp vrf vrf10 ipv4 unicast`
  - `show bgp vrf vrf10 ipv6 unicast`
  - `show ip route vrf vrf10 192.168.3.0/24 json`
  - `show segment-routing srv6 sid`
