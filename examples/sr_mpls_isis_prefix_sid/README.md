# SR-MPLS IS-IS Prefix-SID

This example adds SR-MPLS Prefix-SIDs to the plain three-router IS-IS line.

## Topology

### Routers

- `r1`
  - Loopback: `10.255.81.1/32`
  - Underlay: `eth1 = 10.0.81.0/31`
  - Prefix-SID index: `101`
- `r2`
  - Loopback: `10.255.81.2/32`
  - Underlay:
    - `eth1 = 10.0.81.1/31`
    - `eth2 = 10.0.81.2/31`
  - Prefix-SID index: `102`
- `r3`
  - Loopback: `10.255.81.3/32`
  - Underlay: `eth1 = 10.0.81.3/31`
  - Prefix-SID index: `103`

### Reachability

- `pings:` checks the routed dataplane with `r1 -> r3 loopback`.
- `r1` and `r3` each install one explicit headend route that pushes the remote
  node SID (`16103` / `16101`) toward `r2`.
- Give the lab roughly 20 seconds after `up` so IS-IS adjacency, SID learning,
  and MPLS table programming have all settled.
- Useful follow-up commands:
  - `show isis segment-routing node`
  - `show mpls table`
