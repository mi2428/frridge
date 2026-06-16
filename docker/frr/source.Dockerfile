ARG UBUNTU_VERSION=24.04
FROM ubuntu:${UBUNTU_VERSION}

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG FRR_GIT_REF=master
ARG LIBYANG_GIT_REF=v2.1.148

ENV DEBIAN_FRONTEND=noninteractive

# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install --no-install-recommends -y \
    autoconf \
    automake \
    bison \
    bridge-utils \
    build-essential \
    ca-certificates \
    cmake \
    curl \
    dnsutils \
    ethtool \
    flex \
    git \
    install-info \
    iproute2 \
    iputils-ping \
    jq \
    libc-ares-dev \
    libcap-dev \
    libelf-dev \
    libjson-c-dev \
    libpam0g-dev \
    libpcre2-dev \
    libprotobuf-c-dev \
    libreadline-dev \
    libtool \
    libunwind-dev \
    make \
    mtr-tiny \
    net-tools \
    perl \
    pkg-config \
    procps \
    protobuf-c-compiler \
    python3 \
    python3-dev \
    python3-pip \
    python3-venv \
    tcpdump \
    texinfo \
    tini \
    traceroute \
 && rm -rf /var/lib/apt/lists/*

RUN set -euo pipefail \
 && git clone --depth 1 --branch "${LIBYANG_GIT_REF}" https://github.com/CESNET/libyang.git /tmp/libyang \
 && cmake -S /tmp/libyang -B /tmp/libyang/build \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX=/usr \
    -DENABLE_BUILD_TESTS=OFF \
    -DENABLE_VALGRIND_TESTS=OFF \
 && cmake --build /tmp/libyang/build -j"$(nproc)" \
 && cmake --install /tmp/libyang/build \
 && rm -rf /tmp/libyang

WORKDIR /tmp/frr

RUN set -euo pipefail \
 && git init . \
 && git remote add origin https://github.com/FRRouting/frr.git \
 && git fetch --depth 1 origin "${FRR_GIT_REF}" \
 && git checkout --detach FETCH_HEAD \
 && ./bootstrap.sh \
 && ./configure \
    --prefix=/usr \
    --sysconfdir=/etc \
    --localstatedir=/var \
    --sbindir=/usr/lib/frr \
    --libdir=/usr/lib \
    --enable-user=root \
    --enable-group=root \
    --enable-vty-group=root \
    --enable-vtysh \
    --disable-doc \
    --disable-snmp \
 && make -j"$(nproc)" \
 && make install-strip \
 && rm -rf /tmp/frr

WORKDIR /

COPY docker/frr/docker-start /usr/lib/frr/docker-start

RUN chmod 0755 /usr/lib/frr/docker-start \
 && install -d -m 0755 /etc/frr /var/run/frr /var/log/frr

STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/lib/frr/docker-start"]
