# EVPN Dual-Stack IRB over IPv6 Clos

This example extends the IPv6-underlay Clos IRB pattern into a dual-stack
tenant fabric.
The student VLAN is stretched across all three leaves as one dual-stack L2VNI,
each leaf owns one routed dual-stack services subnet, and the tenant VRF
exports both IPv4 and IPv6 prefixes as EVPN type-5.

## Topology

### Spine Routers

- `sp1`
  - IPv6 loopback: `2001:db8:196::1/128`
  - Role: IPv6 underlay / EVPN route reflector
- `sp2`
  - IPv6 loopback: `2001:db8:196::2/128`
  - Role: IPv6 underlay / EVPN route reflector

### Leaf Routers

- `lf1`
  - IPv6 loopback / VTEP: `2001:db8:196::11/128`
  - Shared student gateway:
    - IPv4: `10.10.50.1/24`
    - IPv6: `2001:db8:196:10::1/64`
    - Anycast MAC: `02:00:00:10:50:01`
  - Local services subnet:
    - IPv4: `10.10.60.0/24`
    - IPv6: `2001:db8:196:20::/64`
- `lf2`
  - IPv6 loopback / VTEP: `2001:db8:196::12/128`
  - Shared student gateway:
    - IPv4: `10.10.50.1/24`
    - IPv6: `2001:db8:196:10::1/64`
    - Anycast MAC: `02:00:00:10:50:01`
  - Local services subnet:
    - IPv4: `10.10.70.0/24`
    - IPv6: `2001:db8:196:30::/64`
- `lf3`
  - IPv6 loopback / VTEP: `2001:db8:196::13/128`
  - Shared student gateway:
    - IPv4: `10.10.50.1/24`
    - IPv6: `2001:db8:196:10::1/64`
    - Anycast MAC: `02:00:00:10:50:01`
  - Local services subnet:
    - IPv4: `10.10.80.0/24`
    - IPv6: `2001:db8:196:40::/64`

## Reachability

- The shared student subnet uses EVPN type-2 for both IPv4 and IPv6 hosts.
- Each leaf exports only its local services subnet as EVPN type-5, for both
  address families.
- `pings:` checks:
  - IPv4 student-to-student across the stretched L2 segment
  - IPv6 student-to-student across the stretched L2 segment
  - IPv4 student-to-service across the anycast gateway and type-5 routing
  - IPv6 service-to-service across remote type-5 routes
