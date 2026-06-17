# EVPN Inter-AS Option B over IPv6 Dual Clos

This example builds two small 2-spine / 3-leaf IPv6 Clos pods and joins them
with EVPN Inter-AS Option B across the two border leaves.
The border leaves do not host tenant endpoints; they relay IPv6 underlay
reachability plus EVPN NLRI between the two fabrics, while the edge leaves
stretch one shared L2 segment end to end.

## Topology

### Left Pod (AS 65001)

- Spines: `lsp1`, `lsp2`
- Access leaves: `llf1`, `llf2`
- Border leaf: `llf3`
- Host namespaces:
  - `llf1 host`: `10.10.193.11/24`
  - `llf2 host`: `10.10.193.12/24`

### Right Pod (AS 65002)

- Spines: `rsp1`, `rsp2`
- Access leaves: `rlf1`, `rlf2`
- Border leaf: `rlf3`
- Host namespaces:
  - `rlf1 host`: `10.10.193.21/24`
  - `rlf2 host`: `10.10.193.22/24`

## Reachability

- Each pod uses an IPv6-only Clos underlay with local EVPN route reflection.
- `llf3` and `rlf3` exchange IPv6 unicast plus EVPN NLRI across an eBGP
  inter-AS link.
- The stretched L2VNI uses a shared EVPN route-target `65193:100` on all
  access leaves so one tenant is imported consistently across both ASes.
- The border leaves also rewrite remote IPv6 loopback reachability toward
  their local spines with `next-hop-self force`, so VTEP recursion resolves
  entirely inside each pod.
- `pings:` checks host reachability from non-border leaves in the left pod to
  non-border leaves in the right pod.
