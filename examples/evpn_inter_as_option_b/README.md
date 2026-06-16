# EVPN Inter-AS Option B

This lab keeps the dataplane small and focuses on the control-plane handoff:
two VTEPs live in different autonomous systems and exchange IPv4 unicast plus
EVPN NLRI over an eBGP session. Each PE advertises its loopback as the VTEP
address and stretches VNI `100` between local host namespaces.

## Topology

- `pe1`
  - AS: `65001`
  - Loopback / VTEP: `10.255.93.11/32`
  - Host namespace: `10.10.93.11/24`
- `pe2`
  - AS: `65002`
  - Loopback / VTEP: `10.255.93.12/32`
  - Host namespace: `10.10.93.12/24`

`pings:` checks host-to-host reachability across the EVPN VXLAN segment.
