# MPLS Carrier's Carrier (4 Routers)

This example splits the topology into `PE -> ASBR -> ASBR -> PE`.
`lab.yaml` keeps the two ASBRs out of the service BGP session and forms the actual inter-AS eBGP adjacency directly between `pe1` and `pe2`. Because this lab is IP-only, the ASBRs also carry static routes for the PE loopbacks and service subnets.

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

- `pe1` and `pe2` form the service eBGP session directly between their loopbacks.
- `asbr1` and `asbr2` do not participate in the service BGP control plane.
- To keep this learning lab IP-only and pingable, the ASBRs carry static routes for both PE loopbacks and both service subnets.
- The service routes are exchanged on the end-to-end multihop PE session instead of on any ASBR-facing BGP session.
- This is the role-split learning version of option C. It intentionally focuses on PE-to-PE service peering rather than on a full MPLS VPN control plane.

### Reachability

- `pings:` in `lab.yaml` checks routed reachability from the AS 65001 host to the AS 65002 host.
- Right after `up`, give BGP a short moment to converge before treating the first ping result as final.
