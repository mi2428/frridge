# SR-MPLS OSPF Prefix-SID

This example is the OSPFv2 counterpart to the IS-IS Prefix-SID lab.

## Topology

### Routers

- `r1`
  - Loopback: `10.255.82.1/32`
  - Underlay: `eth1 = 10.0.82.0/31`
  - Prefix-SID index: `201`
- `r2`
  - Loopback: `10.255.82.2/32`
  - Underlay:
    - `eth1 = 10.0.82.1/31`
    - `eth2 = 10.0.82.2/31`
  - Prefix-SID index: `202`
- `r3`
  - Loopback: `10.255.82.3/32`
  - Underlay: `eth1 = 10.0.82.3/31`
  - Prefix-SID index: `203`

### Reachability

- `pings:` checks `r1 -> r3 loopback`.
- `r1` and `r3` each install one explicit MPLS headend route toward the remote
  node SID (`16203` / `16201`) through `r2`.
- Give the lab roughly 20 seconds after `up` so OSPF adjacency, opaque LSAs,
  and MPLS table programming have all settled.
- Useful follow-up commands:
  - `show ip ospf database segment-routing`
  - `show mpls table`
