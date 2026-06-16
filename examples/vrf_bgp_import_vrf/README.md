# VRF BGP `import vrf` (1 Router)

This example builds one router with two Linux VRFs, `blue` and `red`.
`lab.yaml` redistributes the connected subnet from each VRF into BGP and then uses the FRR `import vrf` shortcut so each VRF learns the other side through the default VPN RIB.

## Topology

### Router

- `rt1`
  - Loopback: `10.255.97.1/32`
  - BGP AS: `65000`
  - VRF `blue`
    - Table: `1101`
    - Router interface: `blue0` with `10.10.97.1/24`
    - Local host netns: `blue-host`, `10.10.97.11/24`
    - BGP behavior:
      - `redistribute connected`
      - `import vrf red`
  - VRF `red`
    - Table: `1102`
    - Router interface: `red0` with `10.20.97.1/24`
    - Local host netns: `red-host`, `10.20.97.11/24`
    - BGP behavior:
      - `redistribute connected`
      - `import vrf blue`

### Reachability

- `pings:` in `lab.yaml` checks whether the `blue-host` namespace can reach the `red-host` namespace after BGP leaks the two connected prefixes across VRFs.
- This is the shortest example in the repo that shows FRR VRF-to-VRF leaking through `import vrf` rather than explicit RD/RT configuration.
