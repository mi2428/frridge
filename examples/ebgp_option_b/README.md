# eBGP Inter-AS Option B

This example is the L3VPN/MPLS version of option B.
`lab.yaml` follows the same control-plane split as FRR's upstream `bgp_vpnv4_asbr` test: local VPN and labeled-unicast state stays inside AS 65001, while VPN routes cross the AS boundary through a separate eBGP-facing speaker.

## Topology

### AS 65001

- `pe1`
  - Loopback: `10.255.1.11/32`
  - Local LAN:
    - VRF `tenant`
    - `host` netns: `172.31.0.10/24`
  - Core-side link:
    - `eth1` on `192.168.0.0/24`
- `asbr1`
  - Loopback: `10.255.1.1/32`
  - Core-side link:
    - `eth1` on `192.168.0.0/24`
  - Inter-AS link:
    - `eth2` on `192.168.1.0/24`
- `rr1`
  - Loopback: `10.255.1.100/32`
  - Route-reflector link:
    - `eth1` on `192.168.0.0/24`

### Boundary

- `rs1`
  - eBGP VPN route server on `192.168.1.200/24`

### AS 65002

- `pe2`
  - Loopback: `10.255.2.12/32`
  - Local LAN:
    - VRF `tenant`
    - `host` netns: `172.31.1.10/24`
  - Inter-AS side link:
    - `eth1` on `192.168.1.0/24`

### Inter-AS Behavior

- `pe1` and `asbr1` send `ipv4 vpn` routes to `rr1` using loopback-based iBGP.
- `pe1` and `asbr1` send `ipv4 labeled-unicast` to `rr1` on the shared `192.168.0.0/24` segment.
- `asbr1` sends VPN routes to `rs1` using eBGP on `192.168.1.0/24`.
- `pe2` learns the remote VPN route from `rs1` using eBGP on `192.168.1.0/24`.
- `asbr1` uses `mpls bgp l3vpn-multi-domain-switching` on both sides and `mpls bgp forwarding` on the inter-AS side.
- `pe2` uses `mpls bgp forwarding` to install the remote VPN prefix without a separate transport label.
- The tenant VRF uses explicit export labels:
  - `pe1`: label `101`, RD `65001:101`, RT `65000:101`
  - `pe2`: label `102`, RD `65002:102`, RT `65000:101`

### Reachability

- `pings:` in `lab.yaml` checks routed reachability from `pe1` host `172.31.0.10` to `pe2` host `172.31.1.10`.
- A successful run should also show:
  - `show bgp ipv4 vpn` on `asbr1` with both VPN routes present
  - `show mpls table` on `asbr1` with a swap entry toward `pe2`
