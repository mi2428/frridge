# frridge

A Linux-first FRR lab runner that creates router containers, wires them together with veth pairs and bridges.

## Installation

### Build from source

Install Go and Docker first, then build and install the binaries with `make install`.
By default, the binaries are installed to `~/.local/bin/frridge` and `~/.local/bin/frridge-mp`.
Set `INSTALL_BINDIR` if you want to install them somewhere else.

```console
$ git clone git@github.com:mi2428/frridge.git
$ make -C frridge install
$ docker build -t frridge-frr:latest ./frridge
```

The bundled `Dockerfile` is optional.
You can also point the topology at `frrouting/frr:<tag>` or another image that already contains FRR and `vtysh`.

>[!TIP]
> Prebuilt tarballs are also available from GitHub Releases for macOS and Linux, with amd64 and arm64 builds for each platform.
> Each archive contains `frridge`, `frridge-mp`, and this README.
> Use `frridge` on a Linux host, or `frridge-mp` on macOS when you want to run the lab inside Multipass.
>
> ```console
> $ curl -L -o frridge.tar.gz https://github.com/mi2428/frridge/releases/download/v0.1.0/frridge-v0.1.0-darwin-arm64.tar.gz
> $ tar -xzf ./frridge.tar.gz
> $ chmod +x ./frridge-v0.1.0-darwin-arm64/frridge-mp
> ```

## Usage

`frridge` talks to Docker and host netlink directly, so the main binary expects a real Linux host.

```console
$ frridge --help

FRR lab runner for container-based network study

Usage:
  frridge [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  console     Open vtysh or a shell inside a running router container
  down        Remove containers and runtime networking for a lab
  help        Help about any command
  ping        Run YAML-defined ping checks and print the raw ping output
  up          Create containers, links, and initial router state

Flags:
  -f, --file string   Path to lab YAML
  -h, --help          help for frridge
  -v, --version       version for frridge

Use "frridge [command] --help" for more information about a command.
```

Create a lab, enter a router, and tear it down again:

```console
$ sudo frridge up -f lab.yaml
$ sudo frridge ping -f lab.yaml
$ sudo frridge ping -f lab.yaml lf1-host-to-lf2-host
$ sudo frridge console rt1 -f lab.yaml
$ sudo frridge console rt1 -f lab.yaml --shell
$ sudo frridge down -f lab.yaml --purge
```

`up` creates containers, wires links, applies optional `linux:` dataplane objects, runs any remaining shell escape hatches, and then applies first-boot `vtysh` seed commands.
`ping` runs the named checks from top-level `pings:` and prints the underlying `ping(8)` output unchanged.
`console` opens `vtysh` by default.
`down --purge` also removes generated lab files under `lab.workdir`.

### macOS via Multipass

Use `frridge-mp` when you want to edit topology files on macOS but execute the lab in a Linux VM.

```console
$ frridge-mp --help

Run frridge labs inside a Multipass Linux VM

Usage:
  frridge-mp [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  console     Open vtysh or a shell inside a running guest-backed router container
  down        Remove containers and runtime networking inside the guest
  exec        Run an arbitrary command inside the guest workspace
  help        Help about any command
  ping        Run YAML-defined ping checks inside the guest
  shell       Open a shell inside the guest at the mounted host workspace
  up          Create containers, links, and initial router state inside the guest

Flags:
      --cpus int          CPU count used when the instance is first launched
      --disk string       Disk size used when the instance is first launched
  -h, --help              help for frridge-mp
      --host-dir string   Host directory mounted into the guest for topology files and lab assets
      --image string      Ubuntu image used when the instance is first launched
      --instance string   Multipass instance name
      --memory string     Memory size used when the instance is first launched
      --repo-dir string   Path to the frridge source tree used to build the guest binary
  -v, --version           version for frridge-mp

Use "frridge-mp [command] --help" for more information about a command.
```

Every `frridge-mp` command auto-creates or updates the guest VM, mounts the
requested host directory, and refreshes the guest-local `frridge` binary as
needed.

The common flow is:

```console
$ frridge-mp up --repo-dir ~/src/frridge --host-dir ../toy-evpn-vxlan --file lab.yaml
$ frridge-mp ping --repo-dir ~/src/frridge --host-dir ../toy-evpn-vxlan --file lab.yaml
$ frridge-mp console --repo-dir ~/src/frridge --host-dir ../toy-evpn-vxlan --file lab.yaml rt1
$ frridge-mp down --repo-dir ~/src/frridge --host-dir ../toy-evpn-vxlan --file lab.yaml --purge
```

>[!NOTE]
> `--host-dir` is the directory mounted into the guest.
> Keep the topology file and any bind-mounted lab assets below that tree.
> Runtime `.frridge` state stays on the guest filesystem rather than on the shared mount.

## Supported YAML

This is the supported `frridge` YAML surface. The comments are the contract.

```yaml
# Required schema version. This is the only version supported today.
apiVersion: frridge/v1alpha1     # required; the only schema version supported today

# Lab-wide metadata, runtime state location, and optional router-default overrides.
# When omitted, frridge fills in:
#   image: frridge-frr:latest
#   privileged: true
#   sysctls:
#     net.ipv4.ip_forward: "1"
#     net.ipv4.conf.all.rp_filter: "0"
lab:
  name: clos-manual              # required; used in container names and generated state paths
  workdir: .frridge              # optional; defaults to .frridge relative to this YAML file
  defaults:                      # optional; overrides the built-in router defaults above
    image: frrouting/frr:v10.6.1 # optional; use this when the whole lab wants a different image
    privileged: false            # optional; router-local values still win if set
    sysctls:                     # optional; merged onto the built-ins, then merged with router sysctls
      net.ipv4.conf.all.rp_filter: "2"

# Router definitions. Each router becomes one container.
routers:
  rt1:
    hostname: spine1             # optional; defaults to the router key (rt1 here)
    image: frrouting/frr:v10.6.1 # optional router-local override for lab.defaults.image or the built-in image
    privileged: true             # optional router-local override for lab.defaults.privileged or the built-in true
    env:                         # optional container environment variables
      ROLE: spine
    loopbacks:                   # optional; each value must be valid CIDR
      - 10.255.0.1/32
      - 2001:db8:ffff::1/128
    mounts:                      # optional additional bind mounts
      - source: ./shared/rt1     # source is relative to this YAML file unless already absolute
        target: /lab             # target must be an absolute path inside the container
        readOnly: false          # optional; defaults to false
    sysctls:                     # optional router-local sysctls merged after built-ins and lab.defaults.sysctls
      net.ipv4.conf.eth1.rp_filter: "0"
    linux:                       # optional router-local Linux dataplane objects built after links and loopbacks
      routes:
        - to: 10.255.0.2/32      # optional static routes in the router namespace
          via: 192.0.2.1         # optional; set via, dev, or both
          dev: eth1
      bridges:
        - name: br10             # optional Linux bridge device
          addresses: [10.10.10.1/24]
          interfaces: [eth2]     # optional existing router interfaces enslaved to the bridge
          vxlans:
            - name: vxlan100
              vni: 100
              local: 10.255.0.1
              nolearning: true
          namespaces:
            - name: host
              ifname: eth0
              mac: 02:00:00:00:10:11
              addresses: [10.10.10.11/24]
              defaultVia: 10.10.10.1
    commands:                    # optional post-link startup commands
      - kind: shell              # shell is the escape hatch; it runs on every `frridge up` after `linux:`
        run: |
          ip netns exec host ping -c 1 -W 1 10.10.10.254 >/dev/null 2>&1 || true
      - kind: vtysh              # vtysh commands run once, then persist in /etc/frr/frr.conf
        run: |
          configure terminal
          router bgp 65000
           bgp router-id 10.255.0.1
           no bgp default ipv4-unicast

  rt2:
    loopbacks:
      - 10.255.0.2/32
    commands:
      - kind: vtysh
        run: |
          configure terminal
          router bgp 65000
           bgp router-id 10.255.0.2
           no bgp default ipv4-unicast

  host1: {}                    # empty router entries are valid and inherit the built-ins plus any lab.defaults

# Link definitions. Use p2p for point-to-point links and bridge for shared segments.
links:
  - name: rt1-rt2              # required; must be unique within the file
    type: p2p                  # required; supported values are p2p and bridge
    mtu: 1500                  # optional; applies to the host-side veth pair and router interfaces
    members:
      - router: rt1            # router must exist under routers:
        ifname: eth1           # required; must be unique per router across all links
        ipv4: 192.0.2.0/31     # optional; configured with `ip addr replace`
        mac: 02:00:00:00:01:01 # optional; configured before the interface is brought up
      - router: rt2
        ifname: eth1
        ipv4: 192.0.2.1/31

  - name: access
    type: bridge               # bridge needs at least two members; p2p needs exactly two
    members:
      - router: rt1
        ifname: eth2
      - router: rt2
        ifname: eth2
      - router: host1
        ifname: eth1

# Optional ping checks. These do nothing during `up`; they are only executed
# when `frridge ping` or `frridge-mp ping` is invoked.
pings:
  - name: rt1-to-rt2-loopback   # required; must be unique within the file
    from:
      router: rt1               # required; ping runs inside this router container
      namespace: host           # optional; runs via `ip netns exec <namespace>` when set
    to: 10.255.0.2              # required; target passed to ping(8) as-is
    count: 5                    # optional; defaults to 3 when omitted
```

### Minimal Manual Lab

If you want to practice FRR by typing all routing config yourself, keep the YAML to routers plus links and omit `commands` entirely.
After `up`, enter routers with `frridge console <router>` and configure them by hand.

```yaml
# Minimal manual lab. Create the containers and links, then enter all FRR
# routing config yourself with `frridge console <router>`.
apiVersion: frridge/v1alpha1

# Lab metadata only. The built-in image/sysctl/privileged defaults are enough
# for a plain manual-routing lab.
lab:
  name: clos-manual

# Empty router entries are enough when you want to configure everything by hand.
routers:
  sp1: {}
  sp2: {}
  lf1: {}
  lf2: {}

# Clos fabric wiring only. No shell or vtysh seed commands are provided.
links:
  - name: lf1-sp1
    type: p2p
    members:
      - { router: lf1, ifname: eth1 }
      - { router: sp1, ifname: eth1 }
  - name: lf1-sp2
    type: p2p
    members:
      - { router: lf1, ifname: eth2 }
      - { router: sp2, ifname: eth1 }
  - name: lf2-sp1
    type: p2p
    members:
      - { router: lf2, ifname: eth1 }
      - { router: sp1, ifname: eth2 }
  - name: lf2-sp2
    type: p2p
    members:
      - { router: lf2, ifname: eth2 }
      - { router: sp2, ifname: eth2 }
```

## Development

`make release TAG=vX.Y.Z` builds four release tarballs, pushes the Git tag, creates or updates the GitHub Release, and uploads the local `dist/` artifacts.
`make mp-verify` runs the Multipass-backed smoke test.
Before releasing, this repository must have a clean working tree.

```console
$ make

Development
  build            Build host binaries into bin/
  install          Build and install host binaries into INSTALL_BINDIR
  fmt              Format Go sources. Use CHECK_ONLY=1 to check without writing
  lint             Run Go static analysis
  test             Run unit tests
  check            Run formatting, lint, and unit tests
  clean            Remove local build artifacts

Multipass
  mp-shell         Open a shell in the Multipass workspace
  mp-verify        Run the Multipass-backed smoke test
  mp-stop          Stop the Multipass VM
  mp-delete        Delete the Multipass VM and purge local Multipass state
  mp-status        Show Multipass VM status

Distribution
  release          Build dist artifacts and publish them to a GitHub release. Requires TAG=vX.Y.Z
  dist             Build release tarballs into dist/. Use OS=darwin,linux and ARCH=amd64,arm64
  dist-smoke       Smoke-test the host-matching dist tarball
  checksums        Write SHA-256 checksums for dist artifacts

Help
  help             Show this help message

Variables:
  TAG              Release tag for make release, for example v0.1.0
  PACKAGE_VERSION  Build version, defaults to git describe or TAG
  GIT_REMOTE       Release git remote, defaults to origin
  OS               Release OS list for make dist, defaults to darwin,linux
  ARCH             Release arch list for make dist, defaults to amd64,arm64
  INSTALL_BINDIR   Install directory, defaults to /Users/teo/.local/bin
  MP_NAME          Multipass instance name, defaults to frridge-dev
  VERIFY_LAB       Topology used by make mp-verify, defaults to testdata/smoke/lab.yaml

Examples:
  make build                                  # Build host binaries with git-describe version metadata
  make check                                  # Run formatting, lint, and unit tests
  make mp-verify                              # Run the Multipass-backed smoke test
  make dist OS=darwin,linux ARCH=amd64,arm64  # Build release tarballs and checksums
  make release TAG=v0.1.0                     # Publish a GitHub release from local dist artifacts
```

## License

MIT.
