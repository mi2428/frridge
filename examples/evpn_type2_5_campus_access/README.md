# EVPN Type-2 + Type-5 Campus Access (1 RR, 2 Leafs, 8 Netshoot Hosts)

This example mixes a stretched access subnet with routed service subnets.
The access VLAN uses EVPN type-2 so student endpoints on different leafs stay in the same L2 segment.
The per-leaf service VLANs are exported as EVPN type-5 prefixes so the student subnet can reach remote services through the distributed anycast gateway.

## Topology

### Route Reflector

- `rr`
  - Loopback: `10.255.95.100/32`
  - Role: IPv4 unicast / EVPN route reflector
  - Underlay links:
    - `eth1` to `lf1 eth1`: `10.0.95.1/31`
    - `eth2` to `lf2 eth1`: `10.0.95.3/31`

### Leaf 1

- `lf1`
  - Loopback / VTEP: `10.255.95.11/32`
  - Underlay link:
    - `eth1` to `rr eth1`: `10.0.95.0/31`
  - Tenant VRF:
    - `tenant`
    - L3VNI: `5000`
  - Access segment:
    - `br10`
    - `vxlan100` with VNI `100`
    - Anycast gateway: `10.10.10.1/24`, MAC `02:00:00:10:10:01`
    - Local netshoot hosts:
      - `student11`: `10.10.10.11/24`
      - `student12`: `10.10.10.12/24`
  - Local routed services subnet:
    - `eth3`: `10.10.20.1/24`
    - Local netshoot hosts:
      - `library11`: `10.10.20.11/24`
      - `library12`: `10.10.20.12/24`

### Leaf 2

- `lf2`
  - Loopback / VTEP: `10.255.95.12/32`
  - Underlay link:
    - `eth1` to `rr eth2`: `10.0.95.2/31`
  - Tenant VRF:
    - `tenant`
    - L3VNI: `5000`
  - Access segment:
    - `br10`
    - `vxlan100` with VNI `100`
    - Anycast gateway: `10.10.10.1/24`, MAC `02:00:00:10:10:01`
    - Local netshoot hosts:
      - `student21`: `10.10.10.21/24`
      - `student22`: `10.10.10.22/24`
  - Local routed services subnet:
    - `eth3`: `10.10.30.1/24`
    - Local netshoot hosts:
      - `dns21`: `10.10.30.11/24`
      - `dns22`: `10.10.30.12/24`

### Reachability

- For compactness, this lab puts the anycast IP/MAC directly on `br10` instead of using a separate macvlan helper device.
- `student11 -> student21` shows the stretched access subnet carried by EVPN type-2.
- `student11 -> dns21` and `student22 -> library11` show student traffic leaving the anycast gateway and following EVPN type-5 routes toward remote service prefixes.
- `library12 -> dns22` shows that the two routed service subnets also reach each other through the tenant IP-VRF.

All non-FRR endpoints use `nicolaka/netshoot:latest`, so `frridge console <host> --shell` drops you straight into a tool-rich Linux container for packet capture and troubleshooting.
