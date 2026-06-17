# MPLS Carrier's Carrier

This example keeps the control plane deliberately small and focuses on the
transport split in a carrier's-carrier-style MPLS chain.
`lab.yaml` forms a multihop eBGP labeled-unicast session directly between
`pe1` and `pe2`, while `asbr1` and `asbr2` only provide MPLS forwarding and
static reachability across the transport path.

## Topology

### AS 65001

- `pe1`
  - Loopback / service BGP endpoint: `10.255.2.11/32`
  - Transport link:
    - `eth1` to `asbr1 eth1`: `192.0.2.0/31`
  - Local LAN:
    - `br10`: `10.10.30.1/24`
    - `host` netns: `10.10.30.11/24`, MAC `02:00:00:00:c0:11`
- `asbr1`
  - Loopback: `10.255.2.1/32`
  - Transport links:
    - `eth1` to `pe1 eth1`: `192.0.2.1/31`
    - `eth2` to `asbr2 eth1`: `192.0.2.2/31`

### AS 65002

- `asbr2`
  - Loopback: `10.255.2.2/32`
  - Transport links:
    - `eth1` to `asbr1 eth2`: `192.0.2.3/31`
    - `eth2` to `pe2 eth1`: `192.0.2.5/31`
- `pe2`
  - Loopback / service BGP endpoint: `10.255.2.12/32`
  - Transport link:
    - `eth1` to `asbr2 eth2`: `192.0.2.4/31`
  - Local LAN:
    - `br20`: `10.20.30.1/24`
    - `host` netns: `10.20.30.12/24`, MAC `02:00:00:00:c0:12`

### Carrier's Carrier Behavior

- `pe1` and `pe2` exchange loopbacks and attached customer subnets in
  `ipv4 labeled-unicast` over a multihop eBGP session.
- `asbr1` and `asbr2` do not run BGP at all in this lab.
- The ASBRs only enable MPLS on their transit interfaces and carry static
  routes for the two PE loopbacks and the two attached service subnets.
- `mpls bgp forwarding` on the PE-facing links lets the end-to-end labeled BGP
  session install usable transport next-hops over the two intermediate ASBRs.
- This is the smallest example in the repo that shows service PEs staying in
  the control plane while intermediate carrier routers only switch labels.

### Reachability

- `pings:` in `lab.yaml` checks routed reachability from the AS 65001 host to the AS 65002 host.
- Right after `up`, give BGP a short moment to converge before treating the first ping result as final.
