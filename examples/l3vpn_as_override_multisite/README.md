# L3VPN AS-Override Multisite

This example shows the classic "same customer AS on multiple sites" problem and
solves it on the provider edge with `as-override`.

## Topology

- Provider AS 65000
  - `rr` reflects `ipv4 labeled-unicast` and `ipv4 vpn`
  - `pe1` connects site 1
  - `pe2` connects site 2
- Customer AS 65010
  - `ce1` advertises `10.10.96.0/24`
  - `ce2` advertises `10.20.96.0/24`

Without `as-override`, each CE would reject the remote site because its own AS
would appear in the received path. Here the PE rewrites that customer AS before
advertising the remote site back to the CE.

## Reachability

`pings:` checks host-to-host and host-to-remote-gateway reachability in both
directions.

Useful follow-up commands:

```console
vtysh -c 'show bgp vrf tenant ipv4 unicast'
vtysh -c 'show bgp ipv4 vpn'
vtysh -c 'show ip route vrf tenant'
```
