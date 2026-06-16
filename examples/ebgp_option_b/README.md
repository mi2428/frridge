# eBGP Inter-AS Option B (4 Routers)

This example splits the topology into `PE -> ASBR -> ASBR -> PE`.
`lab.yaml` keeps one internal BGP hop on each side and one external BGP hop across the AS boundary, so the two ASBRs propagate service prefixes learned from their local PE to the remote PE.

## Topology

### AS 65001

- `pe1`
  - Loopback: `10.255.1.11/32`
  - Internal BGP link:
    - `eth1` to `asbr1 eth1`: `192.0.2.0/31`
  - Local LAN:
    - `br10`: `10.10.20.1/24`
    - `host` netns: `10.10.20.11/24`, MAC `02:00:00:00:b0:11`
- `asbr1`
  - Loopback: `10.255.1.1/32`
  - Internal BGP link:
    - `eth1` to `pe1 eth1`: `192.0.2.1/31`
  - External BGP link:
    - `eth2` to `asbr2 eth1`: `192.0.2.2/31`

### AS 65002

- `asbr2`
  - Loopback: `10.255.2.2/32`
  - External BGP link:
    - `eth1` to `asbr1 eth2`: `192.0.2.3/31`
  - Internal BGP link:
    - `eth2` to `pe2 eth1`: `192.0.2.5/31`
- `pe2`
  - Loopback: `10.255.2.12/32`
  - Internal BGP link:
    - `eth1` to `asbr2 eth2`: `192.0.2.4/31`
  - Local LAN:
    - `br20`: `10.20.20.1/24`
    - `host` netns: `10.20.20.12/24`, MAC `02:00:00:00:b0:12`

### Inter-AS Behavior

- `pe1` peers with `asbr1` using iBGP inside AS 65001.
- `asbr1` peers with `asbr2` using eBGP across the AS boundary.
- `asbr2` peers with `pe2` using iBGP inside AS 65002.
- The ASBRs rewrite next hops toward their local PE, so the local PE can install the remote service subnet without needing border-link knowledge.
- This is the role-split learning version of option B. It demonstrates VPN-like route propagation roles, but it intentionally does not implement full RFC 4364 RD/RT/MPLS signaling.

### Reachability

- `pings:` in `lab.yaml` checks routed reachability from the AS 65001 host to the AS 65002 host.
- Right after `up`, give BGP a short moment to converge before treating the first ping result as final.
