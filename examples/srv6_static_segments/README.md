# SRv6 Static Segments

This example keeps the control plane out of the way and shows the smallest useful
SRv6 dataplane built with Linux `seg6` and `seg6local`.

## Topology

### Routers

- `r1`
  - Underlay: `eth1 = 2001:db8:12::1/64`
  - Access LAN: `br10 = 10.10.70.1/24`
  - Host netns: `10.10.70.11/24`
  - Headend route:
    - `10.30.70.0/24` steered through `fd00:70:2::1 -> fd00:70:3::100`
- `r2`
  - Underlay:
    - `eth1 = 2001:db8:12::2/64`
    - `eth2 = 2001:db8:23::2/64`
  - Transit SID:
    - `fd00:70:2::1/128` with `End`
- `r3`
  - Underlay: `eth1 = 2001:db8:23::3/64`
  - Access LAN: `br30 = 10.30.70.1/24`
  - Host netns: `10.30.70.33/24`
  - Service SID:
    - `fd00:70:3::100/128` with `End.DX4 nh4 10.30.70.33`

### Reachability

- `pings:` checks one bidirectional SRv6-steered flow:
  - `r1 host -> r3 host`
- Reply traffic is also SRv6-steered in the reverse direction.
