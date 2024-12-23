ARG BASE_IMAGE=fedora:36

FROM $BASE_IMAGE
ARG TARGETARCH
RUN echo "install_weak_deps=False" >> /etc/dnf/dnf.conf

RUN dnf install -y \
    audit \
    coreutils \
    curl \
    cloud-utils-growpart \
    device-mapper \
    dosfstools \
    dracut \
    dracut-live \
    dracut-network \
    dracut-squash \
    dhcp-client \
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

COPY --from=quay.io/kairos/framework:latest / /
# This is the version of your image
ENV RELEASE=v9.9.9
# This is the version of the upstream image
ENV FLAVOR_RELEASE=focal
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

# Cleanup
RUN rm -rf /boot/initramfs-*
RUN rm /var/lib/dbus/machine-id || true
RUN rm /etc/hostname || true