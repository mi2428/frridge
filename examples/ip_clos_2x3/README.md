# IP Clos 2x3

This example builds a plain IPv4 Clos with 2 spines and 3 leaves.
`lab.yaml` uses numbered `/31` underlay links, eBGP between every leaf and every spine, and one routed host subnet behind each leaf.

## Topology

### Spine Routers

- `sp1`
  - Loopback: `10.255.0.1/32`
  - ASN: `65101`
  - Underlay links:
    - `eth1` to `lf1 eth1`: `10.0.0.1/31`
    - `eth2` to `lf2 eth1`: `10.0.0.5/31`
    - `eth3` to `lf3 eth1`: `10.0.0.9/31`
- `sp2`
  - Loopback: `10.255.0.2/32`
  - ASN: `65102`
  - Underlay links:
    - `eth1` to `lf1 eth2`: `10.0.0.3/31`
    - `eth2` to `lf2 eth2`: `10.0.0.7/31`
    - `eth3` to `lf3 eth2`: `10.0.0.11/31`

### Leaf Routers

- `lf1`
  - Loopback: `10.255.0.11/32`
  - ASN: `65111`
  - Underlay links:
    - `eth1` to `sp1 eth1`: `10.0.0.0/31`
    - `eth2` to `sp2 eth1`: `10.0.0.2/31`
  - Local LAN:
    - `br10`: `10.10.11.1/24`
    - `host` netns: `10.10.11.11/24`
- `lf2`
  - Loopback: `10.255.0.12/32`
  - ASN: `65112`
  - Underlay links:
    - `eth1` to `sp1 eth2`: `10.0.0.4/31`
    - `eth2` to `sp2 eth2`: `10.0.0.6/31`
  - Local LAN:
    - `br20`: `10.10.12.1/24`
    - `host` netns: `10.10.12.12/24`
- `lf3`
  - Loopback: `10.255.0.13/32`
  - ASN: `65113`
  - Underlay links:
    - `eth1` to `sp1 eth3`: `10.0.0.8/31`
    - `eth2` to `sp2 eth3`: `10.0.0.10/31`
  - Local LAN:
    - `br30`: `10.10.13.1/24`
    - `host` netns: `10.10.13.13/24`

### Reachability

- Each leaf advertises its loopback and local `/24` into eBGP.
- `pings:` checks host-to-host reachability across the fabric:
  - `lf1 -> lf2`
  - `lf1 -> lf3`
  - `lf2 -> lf3`
