ARG UBUNTU_VERSION=24.04
FROM ubuntu:${UBUNTU_VERSION}

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG FRR_CHANNEL=frr-stable

ENV DEBIAN_FRONTEND=noninteractive

# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install --no-install-recommends -y ca-certificates curl tini \
 && mkdir -p /usr/share/keyrings \
 && curl -fsSL https://deb.frrouting.org/frr/keys.gpg -o /usr/share/keyrings/frrouting.gpg \
 && repo_codename="$(. /etc/os-release && printf '%s' "${VERSION_CODENAME}")" \
 && printf '%s\n' "deb [signed-by=/usr/share/keyrings/frrouting.gpg] https://deb.frrouting.org/frr ${repo_codename} ${FRR_CHANNEL}" > /etc/apt/sources.list.d/frr.list \
 && apt-get update \
 && apt-get install --no-install-recommends -y \
    bridge-utils \
    dnsutils \
    ethtool \
    frr \
    frr-pythontools \
    iproute2 \
    iputils-ping \
    jq \
    mtr-tiny \
    net-tools \
    procps \
    python3 \
    python3-pip \
    python3-venv \
    tcpdump \
    traceroute \
 && rm -rf /var/lib/apt/lists/*

COPY docker/frr/docker-start /usr/lib/frr/docker-start

RUN chmod 0755 /usr/lib/frr/docker-start

STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/lib/frr/docker-start"]
