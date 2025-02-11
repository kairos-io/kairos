ARG BASE_IMAGE=fedora:42

FROM $BASE_IMAGE AS base
ARG TARGETARCH
RUN echo "install_weak_deps=False" >> /etc/dnf/dnf.conf

RUN dnf install -y \
    audit \
    coreutils \
    cloud-utils-growpart \
    curl \
    device-mapper \
    dosfstools \
    dhcp-client \
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

RUN mkdir -p /run/lock
RUN touch /usr/libexec/.keep
RUN systemctl enable getty@tty1.service
RUN systemctl enable getty@tty2.service
RUN systemctl enable getty@tty3.service
RUN systemctl enable systemd-networkd
RUN systemctl enable systemd-resolved
RUN systemctl disable dnf-makecache.service
RUN systemctl disable dnf-makecache.timer
RUN systemctl enable sshd


COPY --from=quay.io/kairos/framework:latest-fips / /

# This is the version of your image
ENV RELEASE=v9.9.9
# This is the version of the upstream image
ENV FLAVOR_RELEASE=36
# This is the remote repository where you push your images to. It will be used for upgrades and such to search for latest releases
ENV REGISTRY_AND_ORG="quay.io/YOUR_ORG/fedora-fips"
# This is the model for which the os was built, i.e. rpi4, rpi3 or generic for x86/arm64
ENV MODEL="generic"
# This is the variant of the image, i.e. fips, core, standard
ENV VARIANT="fips"
# Flavor being used, i.e. fedora, ubuntu, alpine
ENV FLAVOR="fedora"
# Family being used, i.e. debian, rhel, arch, suse
ENV FAMILY="rhel"
# This is the architecture of the image, i.e. x86_64, arm64. Gets autofilled by docker
ENV TARGETARCH=$TARGETARCH

# Create the kairos-release file to identify the OS
RUN kairos-agent --debug versioneer os-release-variables > /etc/kairos-release

# Copy the custom dracut config file
COPY dracut.conf /etc/dracut.conf.d/kairos-fips.conf

# Empty the /etc/machine-id file
RUN printf "" > /etc/machine-id || true

## Generate initrd
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && \
            ln -sf "${kernel#/boot/}" /boot/vmlinuz
RUN kernel=$(ls /lib/modules | head -n1) && \
            dracut -v -N -f "/boot/initrd-${kernel}" "${kernel}" && \
            ln -sf "initrd-${kernel}" /boot/initrd && depmod -a "${kernel}"

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac

# Cleanup
RUN rm -rf /boot/initramfs-*
RUN rm /var/lib/dbus/machine-id || true
RUN rm /etc/hostname || true
