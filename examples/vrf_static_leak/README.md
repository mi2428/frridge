# VRF Static Route Leak (1 Router)

This example builds one router with two Linux VRFs, `blue` and `red`.
`lab.yaml` keeps one host namespace behind each VRF and leaks the remote host `/32` into the local VRF with FRR static routes that use `nexthop-vrf`.

## Topology

### Router

- `rt1`
  - Loopback: `10.255.96.1/32`
  - VRF `blue`
    - Table: `1001`
    - Router interface: `blue0` with `10.10.96.1/24`
    - Local host netns: `blue-host`, `10.10.96.11/24`
    - Leaked route:
      - `10.20.96.11/32 via 10.20.96.11 nexthop-vrf red`
  - VRF `red`
    - Table: `1002`
    - Router interface: `red0` with `10.20.96.1/24`
    - Local host netns: `red-host`, `10.20.96.11/24`
    - Leaked route:
      - `10.10.96.11/32 via 10.10.96.11 nexthop-vrf blue`

### Reachability

- `pings:` in `lab.yaml` checks whether the `blue-host` namespace can reach the `red-host` namespace through the leaked static routes.
- This is the smallest example in the repo that shows FRR leaking traffic between Linux VRF-lite domains without using BGP.
