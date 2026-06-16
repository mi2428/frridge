#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
mods_pkg="linux-modules-extra-$(uname -r)"
packages=()

if ! command -v docker >/dev/null 2>&1; then
	packages+=(docker.io ca-certificates)
fi

if ! command -v make >/dev/null 2>&1; then
	packages+=(make)
fi

if apt-cache show "$mods_pkg" >/dev/null 2>&1 &&
	! dpkg-query -W -f='${Status}' "$mods_pkg" 2>/dev/null | grep -q '^install ok installed$'; then
	packages+=("$mods_pkg")
fi

if ((${#packages[@]})); then
	apt-get update
	apt-get install -y "${packages[@]}"
fi

systemctl enable --now docker
install -d -m 0755 /etc/modules-load.d /home/ubuntu/.local/share/frridge-mp
printf '%s\n' vrf mpls_router mpls_iptunnel >/etc/modules-load.d/frridge-mp.conf
while read -r module; do
	[ -n "$module" ] || continue
	modprobe "$module" 2>/dev/null || true
done </etc/modules-load.d/frridge-mp.conf
