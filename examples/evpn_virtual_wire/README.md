# EVPN Virtual Wire

This directory intentionally does not ship a `lab.yaml`.

The FRR source tree currently used by this repository documents and tests EVPN
as a VLAN-based service interface / MAC-VRF model, but does not expose a
corresponding EVPN VPWS / "virtual wire" sample or user-facing CLI in the same
tree. In other words, `frridge` can provide a runnable EVPN virtual LAN sample
today, but not a faithful EVPN virtual wire sample on top of the current FRR
build.

If upstream FRR grows documented EVPN VPWS support, this directory is the place
to add a runnable `lab.yaml`.
