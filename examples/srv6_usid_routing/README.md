# SRv6 uSID Routing

This example keeps the control plane out of the way and shows SRv6 uSID-style
dataplane routing with Linux `seg6` and `seg6local`.

The headend routers encapsulate IPv4 traffic into one compressed SID container:

- `fc00:0:2:100::` means active C-SID `2`, then final service C-SID `100`.
- `fc00:0:2:101::` means active C-SID `2`, then final service C-SID `101`.

The transit router owns the active C-SID block and uses `End` with the
`next-csid` flavor to advance to the next C-SID. The egress routers decapsulate
with `End.DX4`.

## Topology

- `r1`
  - Underlay: `eth1 = 2001:db8:72:12::1/64`
  - Access LAN: `10.10.72.0/24`
  - Host namespace: `10.10.72.11/24`
  - Reverse service SID: `fc00:0:101::/48` with `End.DX4 nh4 10.10.72.11`
  - Headend route to `10.30.72.0/24`:
    - `encap seg6 mode encap segs fc00:0:2:100::`
- `r2`
  - Underlay:
    - `eth1 = 2001:db8:72:12::2/64`
    - `eth2 = 2001:db8:72:23::2/64`
  - Transit uSID block:
    - `fc00:0:2::/48` with `End flavors next-csid lblen 32 nflen 16`
- `r3`
  - Underlay: `eth1 = 2001:db8:72:23::3/64`
  - Access LAN: `10.30.72.0/24`
  - Host namespace: `10.30.72.33/24`
  - Forward service SID: `fc00:0:100::/48` with `End.DX4 nh4 10.30.72.33`
  - Headend route to `10.10.72.0/24`:
    - `encap seg6 mode encap segs fc00:0:2:101::`

## Reachability

`pings:` checks host-to-host IPv4 reachability. Both directions are SRv6
encapsulated, transit through the `End next-csid` behavior on `r2`, and are
decapsulated by `End.DX4` at the egress router.

Useful checks:

- `ip -6 route show table local`
- `ip route get 10.30.72.33`
- `ip netns exec host ping 10.30.72.33`
