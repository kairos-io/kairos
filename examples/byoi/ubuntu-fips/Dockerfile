# Build the custom ubuntu image
FROM ubuntu:24.04 AS base
ARG TARGETARCH
# Don't get asked while running apt commands
ENV DEBIAN_FRONTEND=noninteractive

## THIS comes from the Ubuntu documentation: https://canonical-ubuntu-pro-client.readthedocs-hosted.com/en/latest/tutorials/create_a_fips_docker_image.html
## I've just added "linux-image-fips" in the package list
RUN --mount=type=secret,id=pro-attach-config \
    apt-get update \
    && apt-get install --no-install-recommends -y ubuntu-advantage-tools ca-certificates \
    && pro attach --attach-config /run/secrets/pro-attach-config \
    && apt-get upgrade -y \
    && apt-get install -y openssl libssl1.1 libssl1.1-hmac libgcrypt20 libgcrypt20-hmac strongswan strongswan-hmac openssh-client openssh-server linux-image-fips \
    && pro detach --assume-yes


## Kairos setup
## From documentation: https://kairos.io/docs/reference/build-from-scratch/
RUN mkdir -p /run/lock
RUN mkdir -p /usr/libexec
RUN touch /usr/libexec/.keep

## Kairos required packages
## See: https://github.com/kairos-io/kairos/blob/master/images/Dockerfile.ubuntu-20-lts
RUN apt-get install -y --no-install-recommends \
    conntrack \
    console-data \
    coreutils \
    cryptsetup \
    curl \
    debianutils \
    dmsetup \
    dosfstools \
    dracut \
    dracut-network \
    e2fsprogs \
    efibootmgr \
    file \
    fuse \
    gawk \
    gdisk \
    grub2 \
    grub2-common \
    grub-efi-amd64-bin \
    grub-efi-amd64-signed \
    grub-pc-bin \
    haveged \
    iproute2 \
    iptables \
    isc-dhcp-common \
    jq \
    kbd \
    krb5-locales \
    lldpd \
    lvm2 \
    mdadm \
    nano \
    nbd-client \
    ncurses-term \
    neovim \
    networkd-dispatcher \
    nfs-common \
    open-iscsi \
    openssh-server \
    open-vm-tools \
    os-prober \
    packagekit-tools \
    parted \
    patch \
    policykit-1 \
    publicsuffix \
    qemu-guest-agent \
    rsync \
    shared-mime-info \
    snapd \
    snmpd \
    squashfs-tools \
    sudo \
    systemd \
    systemd-timesyncd \
    thermald \
    ubuntu-advantage-tools \
    xdg-user-dirs \
    xxd \
    xz-utils \
    zerofree \
    zfsutils-linux \
    zstd \
    && apt-get remove -y unattended-upgrades && apt-get clean \
    && apt-get purge --auto-remove -y ubuntu-advantage-tools \
    && rm -rf /var/lib/apt/lists/*

# Copy the Kairos framework files. We use master builds here for fedora. See https://quay.io/repository/kairos/framework?tab=tags for a list
COPY --from=quay.io/kairos/framework:latest-fips / /

# This is the version of your image
ENV RELEASE=v9.9.9
# This is the version of the upstream image
ENV FLAVOR_RELEASE=24.04
# This is the remote repository where you push your images to. It will be used for upgrades and such to search for latest releases
ENV REGISTRY_AND_ORG="quay.io/YOUR_ORG/ubuntu-fips"
# This is the model for which the os was built, i.e. rpi4, rpi3 or generic for x86/arm64
ENV MODEL="generic"
# This is the variant of the image, i.e. fips, core, standard
ENV VARIANT="fips"
# Flavor being used, i.e. fedora, ubuntu, alpine
ENV FLAVOR="ubuntu"
# Family being used, i.e. debian, rhel, arch, suse
ENV FAMILY="debian"
# This is the architecture of the image, i.e. x86_64, arm64. Gets autofilled by docker
ENV TARGETARCH=$TARGETARCH

# Create the kairos-release file to identify the OS
RUN kairos-agent --debug versioneer os-release-variables > /etc/kairos-release
# Empty the /etc/machine-id file
RUN printf "" > /etc/machine-id || true

## Configuration

COPY dracut.conf /etc/dracut.conf.d/kairos-fips.conf
# CLEANUP
# Installing dracut and fips creates this default packages and symlinks and we dont want that
# We want to fully rebuild and link our initrd
RUN rm -Rf /boot/vmlinuz.old # symlink
RUN rm -Rf /boot/vmlinuz.img.old # symlink
RUN rm -Rf /boot/vmlinuz.img # symlink
RUN rm -Rf /boot/initrd.img.old # symlink to wrong initrd (no immucore, no kairos-agent)
RUN rm -Rf /boot/initrd.img # symlink to wrong initrd (no immucore, no kairos-agent)
RUN rm -Rf /boot/initrd.img-* # wrong initrd (no immucore, no kairos-agent)
## Generate initrd
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && \
            ln -sf "${kernel#/boot/}" /boot/vmlinuz
RUN kernel=$(ls /lib/modules | head -n1) && \
            dracut -v -N -f "/boot/initrd-${kernel}" "${kernel}" && \
            ln -sf "initrd-${kernel}" /boot/initrd && depmod -a "${kernel}"
RUN rm -rf /boot/initramfs-*

# Fixup sudo perms
RUN chown root:root /usr/bin/sudo && chmod 4755 /usr/bin/sudo

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac

# Cleanup
RUN rm -rf /var/cache/* && journalctl --vacuum-size=1K && rm /etc/machine-id && rm /var/lib/dbus/machine-id && rm /etc/hostname
RUN rm -rf /boot/initramfs-*
RUN rm /var/lib/dbus/machine-id || true
RUN rm /etc/hostname || true