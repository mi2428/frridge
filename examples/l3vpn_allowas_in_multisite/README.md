# L3VPN Allowas-In Multisite

This is the customer-side answer to the same multisite problem as
`l3vpn_as_override_multisite`.

Both customer sites stay in AS 65010. Instead of having the provider rewrite
the AS path, each CE explicitly accepts one occurrence of its own AS with
`allowas-in 1`.

## Topology

- Provider AS 65000
  - `rr` reflects `ipv4 labeled-unicast` and `ipv4 vpn`
  - `pe1` connects site 1
  - `pe2` connects site 2
- Customer AS 65010
  - `ce1` advertises `10.10.97.0/24`
  - `ce2` advertises `10.20.97.0/24`

## Reachability

`pings:` checks host-to-host and host-to-remote-gateway reachability in both
directions.

Useful follow-up commands:

```console
vtysh -c 'show bgp ipv4 unicast'
vtysh -c 'show bgp vrf tenant ipv4 unicast'
vtysh -c 'show bgp ipv4 vpn'
```
