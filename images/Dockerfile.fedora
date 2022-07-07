ARG BASE_IMAGE=fedora:33

FROM $BASE_IMAGE
ARG K3S_VERSION

RUN echo "install_weak_deps=False" >> /etc/dnf/dnf.conf
RUN dnf install -y \
    NetworkManager \
    squashfs-tools \ 
    dracut-live \
    efibootmgr \
    audit \
    sudo \
    systemd \
    parted \
    dracut \
    e2fsprogs \
    dosfstools \
    coreutils \
    device-mapper \
    grub2 \
    which \
    curl \
    nano \
    nohang-desktop \
    gawk \
    haveged \
    tar \
    openssh-server \
    shim-x64 \
    grub2-pc \
    grub2-efi-x64 \
    grub2-efi-x64-modules \
    rsync && dnf clean all

ENV INSTALL_K3S_VERSION=${K3S_VERSION}
ENV INSTALL_K3S_BIN_DIR="/usr/bin"
RUN curl -sfL https://get.k3s.io > installer.sh
RUN INSTALL_K3S_SKIP_START="true" INSTALL_K3S_SKIP_ENABLE="true" sh installer.sh
RUN INSTALL_K3S_SKIP_START="true" INSTALL_K3S_SKIP_ENABLE="true" sh installer.sh agent
RUN rm -rf installer.sh