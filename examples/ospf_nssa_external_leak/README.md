# OSPF NSSA External Leak (3 Routers)

This example builds an OSPF backbone router, one ABR, and one NSSA-side ASBR.
`lab.yaml` keeps area `0` between `r1` and `abr`, makes area `1` an NSSA between `abr` and `asbr`, and then redistributes one static route on the ASBR so the leaked service prefix can be studied as it moves across the OSPF domain.

## Topology

### Router 1

- `r1`
  - Loopback: `10.255.98.1/32`
  - OSPF area 0 link:
    - `eth1` to `abr eth1`: `10.0.98.0/31`

### Area Border Router

- `abr`
  - Loopback: `10.255.98.2/32`
  - Role: ABR between backbone area `0` and NSSA area `1`
  - OSPF links:
    - `eth1` to `r1 eth1`: `10.0.98.1/31`
    - `eth2` to `asbr eth1`: `10.0.98.2/31`

### NSSA ASBR

- `asbr`
  - Loopback: `10.255.98.3/32`
  - OSPF NSSA link:
    - `eth1` to `abr eth2`: `10.0.98.3/31`
  - Leaked service route:
    - Static route `172.16.98.10/32 via 192.0.2.1`
    - Redistributed into OSPF with `redistribute static`
  - Local service namespace:
    - `svc`
    - Transit link `192.0.2.0/31`
    - Service loopback `172.16.98.10/32`

### Reachability

- `pings:` in `lab.yaml` checks whether `r1` can reach the service loopback behind the NSSA ASBR.
- This is the smallest OSPF example in the repo that shows a non-OSPF route being injected on the NSSA edge and then carried across the rest of the domain.
- Right after `up`, give OSPF around 15-20 seconds to converge and to translate the external route before treating the first ping result as final.
