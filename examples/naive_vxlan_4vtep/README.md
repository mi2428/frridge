# Naive VXLAN 4-VTEP

This example builds four plain Linux VTEPs with no EVPN control plane.
Each TEP uses the kernel VXLAN device directly, and every `vxlan100` interface gets a static flood list for the three remote VTEPs.
There is no FRR config here at all: the overlay comes entirely from `ip link add ... type vxlan` and `bridge fdb append 00:00:00:00:00:00 dev vxlan100 dst ...`.
The topology uses `nicolaka/netshoot:latest` as the container image so the lab can run without first building the repo's FRR companion image.

## Topology

### Underlay

- One shared underlay segment on `172.16.100.0/24`
- `tep1 eth1`: `172.16.100.11/24`
- `tep2 eth1`: `172.16.100.12/24`
- `tep3 eth1`: `172.16.100.13/24`
- `tep4 eth1`: `172.16.100.14/24`

### Overlay VTEPs

- `tep1`
  - VNI: `100`
  - Local VTEP IP: `172.16.100.11`
  - Local host netns: `10.10.100.11/24`
- `tep2`
  - VNI: `100`
  - Local VTEP IP: `172.16.100.12`
  - Local host netns: `10.10.100.12/24`
- `tep3`
  - VNI: `100`
  - Local VTEP IP: `172.16.100.13`
  - Local host netns: `10.10.100.13/24`
- `tep4`
  - VNI: `100`
  - Local VTEP IP: `172.16.100.14`
  - Local host netns: `10.10.100.14/24`

### Reachability

- All four local host namespaces share the same overlay subnet `10.10.100.0/24`.
- Unknown unicast, broadcast, and ARP discovery work because each VTEP installs three static flood-list FDB entries on `vxlan100`.
- `pings:` checks east-west reachability across the four VTEPs without any EVPN signaling.
