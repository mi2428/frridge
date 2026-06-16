# syntax=docker/dockerfile:1.7

ARG UBUNTU_VERSION=24.04
ARG FRR_GIT_REF=master
ARG LIBYANG_GIT_REF=v2.1.148

FROM ubuntu:${UBUNTU_VERSION} AS build

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG FRR_GIT_REF
ARG LIBYANG_GIT_REF

ENV DEBIAN_FRONTEND=noninteractive
ENV CCACHE_DIR=/root/.cache/ccache

WORKDIR /src

# hadolint ignore=DL3008
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
 && apt-get install --no-install-recommends -y \
    autoconf \
    automake \
    bison \
    build-essential \
    ca-certificates \
    ccache \
    cmake \
    curl \
    flex \
    git \
    install-info \
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
    perl \
    pkg-config \
    protobuf-c-compiler \
    python3 \
    python3-dev \
    texinfo \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src/libyang

RUN git init . \
 && git remote add origin https://github.com/CESNET/libyang.git \
 && git fetch --depth 1 origin "${LIBYANG_GIT_REF}" \
 && git checkout --detach FETCH_HEAD

RUN --mount=type=cache,target=/root/.cache/ccache,sharing=locked \
    CC="ccache gcc" CXX="ccache g++" \
    cmake -S . -B build \
      -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_INSTALL_PREFIX=/usr \
      -DENABLE_BUILD_TESTS=OFF \
      -DENABLE_VALGRIND_TESTS=OFF \
 && cmake --build build -j"$(nproc)" \
 && cmake --install build \
 && install -d /opt/frr-root \
 && cmake --install build --prefix /opt/frr-root/usr

WORKDIR /src/frr

RUN git init . \
 && git remote add origin https://github.com/FRRouting/frr.git \
 && git fetch --depth 1 origin "${FRR_GIT_REF}" \
 && git checkout --detach FETCH_HEAD

RUN --mount=type=cache,target=/root/.cache/ccache,sharing=locked \
    ./bootstrap.sh \
 && CC="ccache gcc" CXX="ccache g++" ./configure \
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
 && make DESTDIR=/opt/frr-root install-strip

FROM ubuntu:${UBUNTU_VERSION} AS runtime

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ENV DEBIAN_FRONTEND=noninteractive

# hadolint ignore=DL3008
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
 && apt-get install --no-install-recommends -y \
    bridge-utils \
    ca-certificates \
    curl \
    dnsutils \
    ethtool \
    iproute2 \
    iputils-ping \
    jq \
    libatomic1 \
    libc-ares2 \
    libcap2 \
    libelf1t64 \
    libjson-c5 \
    libpam0g \
    libpcre2-8-0 \
    libprotobuf-c1 \
    libreadline8t64 \
    libunwind8 \
    mtr-tiny \
    net-tools \
    procps \
    python3 \
    python3-pip \
    python3-venv \
    tcpdump \
    tini \
    traceroute \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /opt/frr-root/ /
COPY docker/frr/docker-start /usr/lib/frr/docker-start

RUN chmod 0755 /usr/lib/frr/docker-start \
 && install -d -m 0755 /etc/frr /var/run/frr /var/log/frr

STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/lib/frr/docker-start"]
