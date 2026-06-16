# OSPFv3 IPv4 Address Family

This directory intentionally has no `lab.yaml`.

With the FRR version currently used by this repository (`10.6.1`), `ospf6d` does not accept `address-family ipv4 unicast` under `router ospf6`, so a working OSPFv3-for-IPv4 sample cannot be provided here yet.

The missing feature is tracked upstream as FRRouting issue `#6800`, "Support of Address Families in OSPFv3 (RFC 5838)".

When FRR grows RFC 5838 support in a release we can actually run here, this directory is the intended place for a real `ospfv3_afipv4` example.
