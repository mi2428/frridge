# VPNv4 RR Add-Path Path Hiding

This example turns the plain `ibgp_rr_addpath` idea into an MPLS L3VPN lab.
`ingress` learns the same remote tenant subnet from `exit1` and `exit2`, while
the route reflector only sends both VPNv4 paths toward the ingress PE.

## Topology

- `ingress`
  - Tenant host subnet: `10.10.91.0/24`
  - Loopback / VPNv4 endpoint: `10.255.91.11/32`
- `rr`
  - Route reflector for `ipv4 labeled-unicast` and `ipv4 vpn`
  - Loopback: `10.255.91.100/32`
- `exit1`, `exit2`
  - Two service-side PEs advertising the same tenant subnet `10.30.91.0/24`
  - Loopbacks: `10.255.91.21/32`, `10.255.91.22/32`
- `service`
  - Shared service host on `10.30.91.33/24`

The control-plane split is deliberate:

- `ipv4 labeled-unicast` rides on the shared core segment and gives each PE a
  label-switched path to the remote PE loopbacks.
- `ipv4 vpn` rides on loopback-based iBGP sessions to the RR.
- The RR reflects both exit paths only to `ingress` with `addpath-tx-all-paths`,
  which is the part that removes the usual path-hiding behavior.

## Reachability

`pings:` checks four paths:

- tenant host -> shared service host
- tenant host -> `exit1` tenant-side gateway
- tenant host -> `exit2` tenant-side gateway
- shared service host -> tenant host

Useful follow-up commands:

```console
vtysh -c 'show bgp ipv4 vpn 10.30.91.0/24'
vtysh -c 'show bgp ipv4 vpn rd all'
vtysh -c 'show ip route vrf tenant 10.30.91.0/24'
vtysh -c 'show mpls table'
```
