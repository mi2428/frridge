# EVPN Type-5 Inter-AS Option B

This example extends the existing inter-AS EVPN pattern from a stretched L2VNI
to a routed L3VNI.
Each PE keeps one tenant VRF with L3VNI `5000` and exports its local connected
subnet into EVPN as a type-5 route.
The two ASBRs exchange IPv4 unicast plus EVPN NLRI, but do not create the
tenant VRF or participate in the tenant dataplane themselves.

## Topology

### AS 65001

- `pe1`
  - Loopback / VTEP: `10.255.95.11/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Local subnet: `10.10.95.0/24`
  - Host namespace: `10.10.95.11/24`
- `asbr1`
  - Loopback: `10.255.95.1/32`
  - Role: EVPN / IPv4 unicast inter-AS handoff

### AS 65002

- `asbr2`
  - Loopback: `10.255.95.2/32`
  - Role: EVPN / IPv4 unicast inter-AS handoff
- `pe2`
  - Loopback / VTEP: `10.255.95.12/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Local subnet: `10.20.95.0/24`
  - Host namespace: `10.20.95.12/24`

## Reachability

- `pe1` exports `10.10.95.0/24` into EVPN as a type-5 route.
- `pe2` exports `10.20.95.0/24` the same way.
- The ASBRs keep `bgp retain route-target all` so they can relay the EVPN
  routes even though they do not import the tenant RT into any local VRF.
- `pings:` checks routed host-to-host reachability through the inter-AS L3VNI.

Useful checks:

- `show bgp l2vpn evpn route type prefix`
- `show bgp vrf tenant ipv4 unicast`
- `show ip route vrf tenant`
- `show evpn vni detail`
