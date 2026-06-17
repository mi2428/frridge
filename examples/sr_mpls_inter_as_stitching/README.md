# SR-MPLS Inter-AS Stitching

This example builds two small SR-MPLS islands and stitches them across an
inter-AS IP handoff between the two ASBRs.
Inside each AS, IS-IS advertises Prefix-SIDs for the PE, the transit router,
and the ASBR.
The edge routers install explicit MPLS headend routes toward their local ASBR,
while each ASBR re-encapsulates the inter-AS handoff traffic into the remote
domain's SR-MPLS dataplane.

## Topology

```text
left host -- pe1 -- p1 -- asbr1 == IP handoff == asbr2 -- p2 -- pe2 -- right host
```

### AS 65001

- `pe1`
  - Loopback: `10.255.161.11/32`
  - Prefix-SID index: `111`
  - Local host namespace: `10.10.161.11/24`
- `p1`
  - Loopback: `10.255.161.1/32`
  - Prefix-SID index: `101`
- `asbr1`
  - Loopback: `10.255.161.2/32`
  - Prefix-SID index: `102`

### AS 65002

- `asbr2`
  - Loopback: `10.255.162.2/32`
  - Prefix-SID index: `202`
- `p2`
  - Loopback: `10.255.162.1/32`
  - Prefix-SID index: `201`
- `pe2`
  - Loopback: `10.255.162.12/32`
  - Prefix-SID index: `212`
  - Local host namespace: `10.20.162.12/24`

## Reachability

- `pe1` pushes the local ASBR node SID (`16102`) for the remote host subnet.
- `asbr1` pops that local SR label, forwards the payload as plain IPv4 across
  the inter-AS link, and `asbr2` re-encapsulates toward the remote PE node SID
  (`16212`).
- The reverse direction does the same with `16202` on `pe2` and `16111` on
  `asbr1`.
- `pings:` checks end-to-end host reachability across the stitched SR domains.
- Give the lab roughly 20 seconds after `up` so both IS-IS domains have time
  to converge before running the ping checks.

Useful checks:

- `show isis segment-routing node`
- `show ip route`
- `show mpls table`
