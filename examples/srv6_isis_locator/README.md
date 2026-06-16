# SRv6 IS-IS Locator

This example advertises SRv6 locators and loopbacks through IS-IS over an IPv6
underlay.

## Topology

### Routers

- `r1`
  - Loopback: `2001:db8:84::1/128`
  - SRv6 transport: `sr0 = 2001:db8:84:100::1/128`
  - Locator: `fd00:84:1::/64`
  - Underlay: `eth1 = 2001:db8:84:12::1/127`
- `r2`
  - Loopback: `2001:db8:84::2/128`
  - SRv6 transport: `sr0 = 2001:db8:84:100::2/128`
  - Locator: `fd00:84:2::/64`
  - Underlay:
    - `eth1 = 2001:db8:84:12::0/127`
    - `eth2 = 2001:db8:84:23::0/127`
- `r3`
  - Loopback: `2001:db8:84::3/128`
  - SRv6 transport: `sr0 = 2001:db8:84:100::3/128`
  - Locator: `fd00:84:3::/64`
  - Underlay: `eth1 = 2001:db8:84:23::1/127`

### Reachability

- `pings:` checks `r1 -> r3 loopback`.
- `up` intentionally waits for a delayed SRv6 rearm so the locator routes are
  present before `frridge ping` runs.
- Useful follow-up commands:
  - `show segment-routing srv6 locator`
  - `show ipv6 route isis`
