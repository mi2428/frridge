# BGP-LS controller-stub SR Policy Controller Stub

This example builds a 3-router IS-IS level-2 line and one BGP-LS controller-stub collector.
`lab.yaml` uses the built-in `frridge-frr:latest` image, which now builds the
latest FRR source from the repo-root `Dockerfile`.

Before starting the lab on a Linux host, build or refresh that companion image:

```console
$ make image
```

When using `frridge-mp` from macOS, the guest auto-builds the image if it is
missing. To refresh it explicitly, run:

```console
$ ./bin/frridge-mp --repo-dir "$PWD" --host-dir "$PWD" exec -- make image
```

## Topology

### Router 1

- `r1`
  - Loopback: `10.255.99.1/32`
  - IS-IS link:
    - `eth1` to `r2 eth1`
  - TE contribution:
    - `mpls-te on`
    - `mpls-te router-address 10.255.99.1`
    - `link-params` on `eth1`

### Router 2

- `r2`
  - Loopback: `10.255.99.2/32`
  - Role: IS-IS transit router and BGP-LS controller-stub speaker
  - IS-IS links:
    - `eth1` to `r1 eth1`
    - `eth2` to `r3 eth1`
  - TE export:
    - `mpls-te on`
    - `mpls-te router-address 10.255.99.2`
    - `mpls-te export`
    - `link-params` on `eth1` and `eth2`
  - BGP-LS controller-stub session:
    - `eth3` to `collector eth1`

### Router 3

- `r3`
  - Loopback: `10.255.99.3/32`
  - IS-IS link:
    - `eth1` to `r2 eth2`
  - TE contribution:
    - `mpls-te on`
    - `mpls-te router-address 10.255.99.3`
    - `link-params` on `eth1`

### Collector

- `collector`
  - Role: BGP-LS controller-stub consumer
  - Outcome:
    - Learn IS-IS Node, Link, and Prefix NLRIs from `r2`

### Reachability

- `pings:` in `lab.yaml` checks IPv4 reachability from `r1` to the `r3`
  loopback over IS-IS.
- The interesting control-plane state is on `collector`, where
  `show bgp link-state link-state` should list the exported Node, Link, and
  Prefix NLRIs for the IS-IS domain.
- Right after `up`, give IS-IS and the BGP session around 20-30 seconds to
  converge before treating the first ping result as final.
