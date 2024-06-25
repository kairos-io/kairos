ARG BASE_IMAGE=fedora:36
FROM $BASE_IMAGE as base

# Generate os-release file
FROM quay.io/kairos/osbuilder-tools:latest as osbuilder
RUN zypper install -y gettext && zypper clean
RUN mkdir /workspace
COPY --from=base /etc/os-release /workspace/os-release
# You should change the following values according to your own versioning and other details
RUN OS_NAME=kairos-core-fedora-fips \
  OS_VERSION=v9.9.9 \
  OS_ID="kairos" \
  OS_NAME=kairos-fedora-fips \
  BUG_REPORT_URL="https://github.com/YOUR_ORG/YOUR_PROJECT/issues" \
  HOME_URL="https://github.com/YOUR_ORG/YOUR_PROJECT" \
  OS_REPO="quay.io/YOUR_ORG/fedora-fips" \
  OS_LABEL="latest" \
  GITHUB_REPO="YOUR_ORG/YOUR_PROJECT" \
  VARIANT="fips" \
  FLAVOR="fedora" \
  /update-os-release.sh

FROM base

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
    lvm2 \
    nano \
    openssh-server \
    parted \
    polkit \
    qemu-guest-agent \
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

RUN mkdir -p /run/lock && \
  touch /usr/libexec/.keep && \
  systemctl enable getty@tty1.service && \
  systemctl enable getty@tty2.service && \
  systemctl enable getty@tty3.service && \
  systemctl enable systemd-networkd && \
  systemctl enable systemd-resolved && \
  systemctl enable sshd

# Copy the os-release file to identify the OS
COPY --from=osbuilder /workspace/os-release /etc/os-release

COPY --from=quay.io/kairos/framework:master_fips-systemd / /

# Copy the custom dracut config file
COPY dracut.conf /etc/dracut.conf.d/kairos-fips.conf

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

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac

RUN rm -rf /boot/initramfs-*
