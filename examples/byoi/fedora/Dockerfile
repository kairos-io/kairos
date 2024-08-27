ARG BASE_IMAGE=fedora:42@sha256:1e59754f0ddb68dabdcd291003a71b64164a363da6479f4d1f12cd7e7c5c1f0f

FROM $BASE_IMAGE

RUN echo "install_weak_deps=False" >> /etc/dnf/dnf.conf

RUN dnf install -y \
    audit \
    coreutils \
    curl \
    device-mapper \
    dosfstools \
    dracut \
    dracut-live \
    dracut-network \
    dracut-squash \
    e2fsprogs \
    efibootmgr \
    gawk \
    gdisk \
    grub2 \
    grub2-efi-x64 \
    grub2-efi-x64-modules \
    grub2-pc \
    haveged \
    kernel \
    kernel-modules \
    kernel-modules-extra \
    livecd-tools \
    nano \
    NetworkManager \
    openssh-server \
    parted \
    polkit \
    rsync \
    shim-x64 \
    squashfs-tools \ 
    sudo \
    systemd \
    systemd-networkd \
    systemd-resolved \
    tar \
    which \
    && dnf clean all

RUN mkdir -p /run/lock
RUN touch /usr/libexec/.keep

COPY --from=quay.io/kairos/framework:master_fedora / /

# Activate Kairos services
RUN systemctl enable cos-setup-reconcile.timer && \
          systemctl enable cos-setup-fs.service && \
          systemctl enable cos-setup-boot.service && \
          systemctl enable cos-setup-network.service

## Generate initrd
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && \
            ln -sf "${kernel#/boot/}" /boot/vmlinuz
RUN kernel=$(ls /lib/modules | head -n1) && \
            dracut -v -N -f "/boot/initrd-${kernel}" "${kernel}" && \
            ln -sf "initrd-${kernel}" /boot/initrd && depmod -a "${kernel}"
RUN rm -rf /boot/initramfs-*
