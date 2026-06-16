# EVPN Type-5 Centralized Services

This example turns the basic EVPN type-5 setup into a simple centralized
services fabric.

## Topology

- `rr`
  - EVPN / IPv4 unicast route reflector
- `branch1`
  - Tenant subnet `10.10.98.0/24`
- `branch2`
  - Tenant subnet `10.20.98.0/24`
- `services`
  - Shared services subnet `10.30.98.0/24`

All three VTEPs share L3VNI `5000` and export their local connected subnet into
EVPN with `advertise ipv4 unicast`.

## Reachability

`pings:` checks:

- branch 1 -> services
- branch 2 -> services
- services -> branch 1
- services -> branch 2
- branch 1 -> branch 2

Useful follow-up commands:

```console
vtysh -c 'show bgp l2vpn evpn route type prefix'
vtysh -c 'show bgp vrf tenant ipv4 unicast'
vtysh -c 'show evpn vni detail'
```
