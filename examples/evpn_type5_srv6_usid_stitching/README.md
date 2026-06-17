# EVPN Type-5 / SRv6 uSID Stitching

This example keeps the same control-plane handoff as
`evpn_type5_srv6_l3vpn_stiching`, but changes the SRv6 core to uSID-flavored
locators.
Each gateway allocates one dynamic per-VRF uSID service SID with
`sid vpn per-vrf export auto`, then stitches EVPN type-5 routes on the
site-facing side to VPNv4 routes on the core-facing side.

## Topology

```text
left host -- le1 == EVPN Type-5/L3VNI == gw1 -- SRv6 uSID L3VPN -- gw2 == EVPN Type-5/L3VNI == re1 -- right host
```

### Left EVPN Site

- `le1`
  - AS: `65010`
  - Loopback / VTEP: `10.255.151.11/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Local tenant subnet: `10.10.151.0/24`
  - Host namespace: `10.10.151.11/24`
- `gw1`
  - AS: `65010`
  - EVPN VTEP: `10.255.151.1/32`
  - SRv6 uSID locator: `fcbb:56:1::/48`

### Right EVPN Site

- `gw2`
  - AS: `65020`
  - EVPN VTEP: `10.255.152.1/32`
  - SRv6 uSID locator: `fcbb:56:2::/48`
- `re1`
  - AS: `65020`
  - Loopback / VTEP: `10.255.152.11/32`
  - Tenant VRF: `tenant`
  - L3VNI: `5000`
  - Local tenant subnet: `10.20.152.0/24`
  - Host namespace: `10.20.152.11/24`

## Reachability

- `le1` exports `10.10.151.0/24` into the left EVPN site as a type-5 route.
- `gw1` imports that route into the tenant VRF, then exports the tenant VRF
  into VPNv4 with a dynamic per-VRF uSID service SID.
- `gw2` imports the VPNv4 route into its tenant VRF and advertises it into the
  right EVPN site as a type-5 route.
- The reverse direction uses the remote gateway's dynamic uSID service SID in
  the same way.

Useful checks:

- `show bgp l2vpn evpn route type 5`
- `show bgp ipv4 vpn`
- `show bgp vrf tenant ipv4 unicast`
- `show segment-routing srv6 sid`
- `show ip route vrf tenant`
