# BGP SRv6 uSID L3VPN

This example extends the existing SRv6 L3VPN pattern with uSID locators. Each
PE allocates VPN service SIDs from a `/48` locator that uses `behavior usid`
and `format usid-f3216`.

## Topology

- `r1`
  - AS `65001`
  - uSID locator `fcbb:0:1::/48`
  - VRFs `vrf10`, `vrf20`
- `r2`
  - AS `65002`
  - uSID locator `fcbb:0:2::/48`
  - VRFs `vrf10`, `vrf20`
- `r3`
  - AS `65001`
  - uSID locator `fcbb:0:3::/48`
  - VRFs `vrf10`, `vrf20`

Inside each VRF, `sid vpn per-vrf export auto` asks Zebra to allocate one
service SID shared by both IPv4 and IPv6 export for that VRF.

## Reachability

`pings:` checks IPv4 customer reachability through two different VPN contexts
and also keeps one underlay loopback probe for sanity.

Useful checks:

- `show segment-routing srv6 locator`
- `show segment-routing srv6 sid`
- `show bgp ipv4 vpn`
- `show bgp ipv6 vpn`
- `show ip route vrf vrf10 192.168.2.0/24 json`
