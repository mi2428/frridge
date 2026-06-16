# EVPN Type-5 / SRv6 L3VPN Stitching

This example stitches two small EVPN Type-5 sites through an SRv6 L3VPN core.
The directory intentionally keeps the short name `stiching` to match the lab
slug, but the topology demonstrates EVPN/SRv6 L3VPN stitching.

## Topology

```text
left host -- le1 == EVPN Type-5/L3VNI == gw1 -- SRv6 L3VPN -- gw2 == EVPN Type-5/L3VNI == re1 -- right host
```

### Left EVPN Site

- `le1`
  - AS: `65010`
  - Loopback / VTEP: `10.255.101.11/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Local tenant subnet: `10.10.101.0/24`
  - Host namespace: `10.10.101.11/24`
- `gw1`
  - AS: `65010`
  - EVPN VTEP: `10.255.101.1/32`
  - SRv6 locator: `2001:db8:56:1::/64`
  - Role: EVPN Type-5 to VPNv4/SRv6 gateway

### SRv6 L3VPN Core

- `gw1` and `gw2` peer over IPv6 using VPNv4.
- Both gateways configure a tenant VRF with:
  - `sid vpn export 1`
  - `rt vpn both 65000:5000`
  - `import vpn`
  - `export vpn`
- The SRv6 service SIDs provide the L3VPN handoff between the two EVPN sites.
- The gateways also install explicit Linux `local ... End.DT4` routes for the
  exported service SIDs and disable per-interface IPv4 `rp_filter` so decapped
  tenant traffic can be forwarded through the VRF dataplane.

### Right EVPN Site

- `gw2`
  - AS: `65020`
  - EVPN VTEP: `10.255.102.1/32`
  - SRv6 locator: `2001:db8:56:2::/64`
  - Role: VPNv4/SRv6 to EVPN Type-5 gateway
- `re1`
  - AS: `65020`
  - Loopback / VTEP: `10.255.102.11/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Local tenant subnet: `10.20.102.0/24`
  - Host namespace: `10.20.102.11/24`

## Reachability

- `le1` advertises `10.10.101.0/24` into the left EVPN site as an EVPN
  type-5 route.
- `gw1` imports the EVPN route into the tenant VRF and exports the tenant VRF
  into VPNv4 with an SRv6 service SID.
- `gw2` imports the VPNv4/SRv6 route into its tenant VRF and advertises it
  into the right EVPN site as an EVPN type-5 route.
- The reverse direction follows the same pattern from `re1` through `gw2`,
  SRv6 L3VPN, `gw1`, and `le1`.

`pings:` checks the transport and service layers:

- `left-evpn-edge-loopback`: left EVPN edge loopback reachability
- `right-evpn-edge-loopback`: right EVPN edge loopback reachability
- `srv6-core-link`: direct IPv6 reachability across the SRv6 core link
- `srv6-locator-reachability`: routed SRv6 locator reachability
- `left-host-to-right-host`: `10.10.101.11 -> 10.20.102.11`
- `right-host-to-left-host`: `10.20.102.11 -> 10.10.101.11`

## Useful Commands

On the EVPN edge routers:

```console
show bgp l2vpn evpn route type 5
show bgp vrf tenant ipv4 unicast
show ip route vrf tenant
```

On the SRv6 gateways:

```console
show bgp l2vpn evpn route type 5
show bgp ipv4 vpn
show bgp vrf tenant ipv4 unicast
show segment-routing srv6 sid
show ip route vrf tenant
```
