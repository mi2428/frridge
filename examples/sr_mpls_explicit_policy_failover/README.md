# SR-MPLS Explicit Policy Failover

This example uses `pathd` to steer a loopback route across a four-router
diamond with an explicit segment list. It is useful for observing a programmed
SR policy and then manually changing links or metrics.

## Topology

### Routers

- `r1`
  - Loopback: `10.255.83.1/32`
  - Headend policy:
    - Color `100`, endpoint `10.255.83.3`
    - Segment list `16302 -> 16303`
- `r2`
  - Loopback: `10.255.83.2/32`
  - Upper transit node on the preferred path
- `r3`
  - Loopback: `10.255.83.3/32`
  - Reverse headend policy:
    - Color `200`, endpoint `10.255.83.1`
    - Segment list `16302 -> 16301`
- `r4`
  - Loopback: `10.255.83.4/32`
  - Lower transit node that remains available in the IGP but is not selected by
    the explicit policy

### Reachability

- `pings:` checks `r1 -> r3 loopback`.
- Give the lab roughly 25 seconds after `up` so IS-IS, TED export, and pathd
  policy installation have all settled.
- Useful follow-up commands:
  - `show sr-te policy detail`
  - `show isis mpls-te database`
  - `show mpls table`
