# Skeleton Labs

These labs stop at the point where the containers exist, the links are wired,
loopbacks are configured, and the underlay IPv4 plan is already applied.
They do **not** seed any FRR routing config.

They intentionally use `frridge-frr:latest`, matching the rest of the
repository's FRR labs.
On Multipass, `frridge-mp` prepares that image inside the guest automatically,
so a fresh VM can still run these labs directly.
They also keep `lab.defaults.privileged: true`, so interface setup and FRR
daemon startup behave the same way as the other examples.

Use them as a kit for `vtysh` practice:

- open a router with `frridge console <router> -f ...`
- configure OSPF, IS-IS, BGP, EVPN, route-reflectors, or policy yourself
- use `frridge ping` to verify the directly connected links that should work before routing converges

Available topologies:

- `two_routers`
  - One point-to-point /31 link.
  - Smallest useful lab for static routes, single-session eBGP, or iBGP basics.
- `three_routers_line`
  - A daisy chain.
  - Good for static routing, OSPF basics, or transit-AS exercises.
- `three_routers_triangle`
  - A 3-node triangle.
  - Good for IGP behavior, ECMP, and simple loop-avoidance exercises.
- `four_routers_ring`
  - A 4-node ring.
  - Good for ring convergence and path-failure experiments.
- `four_routers_full_mesh`
  - Every router directly connected to every other router.
  - Good for iBGP full-mesh or policy experiments without an IGP dependency.
- `hub_and_spoke_4`
  - One hub with three spokes.
  - Good for default-route, route-reflector, or central-policy exercises.
- `leaf_spine_2x2`
  - Two leaves and two spines.
  - Small Clos underlay for eBGP or IGP practice.
- `leaf_spine_3x2`
  - Three leaves and two spines.
  - Slightly larger Clos underlay for route-reflector or EVPN underlay practice.

Every sample lives in its own directory with one `lab.yaml`.
