# EVPN PIM BUM Underlay

This example keeps EVPN for MAC route exchange, but sends BUM replication over
an IPv4 PIM underlay multicast tree instead of ingress replication.

## Topology

- `rr`
  - Route reflector and rendezvous point
  - Loopback `10.255.81.100/32`
- `pe1`
  - VTEP `10.255.81.11`
  - Host namespace `10.10.81.11/24`
- `pe2`
  - VTEP `10.255.81.12`
  - Host namespace `10.10.81.12/24`

Both PEs build one VXLAN bridge for VNI `100` and bind it to multicast group
`239.1.1.100`. `pimd` is started explicitly in the sample so the lab stays
self-contained even though frridge only enables a smaller default daemon set.

## Reachability

This sample is best inspected from the control-plane side. In local validation,
the important pieces were:

- PIM neighborships on `rr`, `pe1`, and `pe2`
- the expected RP mapping for `239.1.1.0/24`
- EVPN MAC routes for the attached host namespaces
- the VNI multicast group binding shown by `show evpn vni detail`

Useful checks:

- `show ip pim neighbor`
- `show ip pim join`
- `show ip pim upstream`
- `show bgp l2vpn evpn route`
- `show bgp l2vpn evpn route type 2`
- `show evpn vni detail`
- `bridge fdb show dev vxlan100`
