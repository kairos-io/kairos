ARG LUET_VERSION=0.16.7
FROM quay.io/luet/base:$LUET_VERSION AS luet

FROM opensuse/leap:15.3
ARG K3S_VERSION=v1.22.2+k3s1
ARG ARCH=amd64
ENV ARCH=${ARCH}
RUN zypper in -y \
    bash-completion \
    conntrack-tools \
    coreutils \
    curl \
    device-mapper \
    dosfstools \
    dracut \
    e2fsprogs \
    findutils \
    gawk \
    gptfdisk \
    grub2-i386-pc \
    grub2-x86_64-efi \
    haveged \
    iproute2 \
    iptables \
    iputils \
    issue-generator \
    jq \
    kernel-default \
    kernel-firmware-bnx2 \
    kernel-firmware-i915 \
    kernel-firmware-intel \
    kernel-firmware-iwlwifi \
    kernel-firmware-mellanox \
    kernel-firmware-network \
    kernel-firmware-platform \
    kernel-firmware-realtek \
    less \
    lsscsi \
    lvm2 \
    mdadm \
    multipath-tools \
    nano \
    nfs-utils \
    open-iscsi \
    open-vm-tools \
    parted \
    pigz \
    policycoreutils \
    procps \
    python-azure-agent \
    qemu-guest-agent \
    rng-tools \
    rsync \
    squashfs \
    strace \
    systemd \
    systemd-sysvinit \
    tar \
    timezone \
    vim \
    which

# Copy the luet config file pointing to the upgrade repository
COPY conf/luet.yaml /etc/luet/luet.yaml

# Copy luet from the official images
COPY --from=luet /usr/bin/luet /usr/bin/luet
RUN luet install -y \
       toolchain/yip \
       toolchain/luet \
       utils/installer \
       system/cos-setup \
       system/immutable-rootfs \
       system/grub2-config \
       system/base-dracut-modules \
       utils/k9s \
       utils/nerdctl

ENV INSTALL_K3S_VERSION=${K3S_VERSION}
ENV INSTALL_K3S_BIN_DIR="/usr/bin"
RUN curl -sfL https://get.k3s.io > installer.sh
RUN INSTALL_K3S_SKIP_START="true" INSTALL_K3S_SKIP_ENABLE="true" sh installer.sh
RUN INSTALL_K3S_SKIP_START="true" INSTALL_K3S_SKIP_ENABLE="true" sh installer.sh agent
RUN rm -rf installer.sh
COPY files/ /

RUN mkinitrd

ARG OS_NAME=c3OS
ARG OS_VERSION=${K3S_VERSION}
ARG OS_REPO=quay.io/mudler/c3os
ARG OS_LABEL=latest

RUN envsubst >/etc/os-release </usr/lib/os-release.tmpl && \
    rm /usr/lib/os-release.tmpl
