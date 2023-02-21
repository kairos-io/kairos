---
title: "Build Kairos"
linkTitle: "Build"
weight: 5
description: >
---

{{% alert title="Note" %}}

This section is reserved for experienced users and advanced use-cases, for instance when building new flavors for Kairos core images. 
Core Kairos images are pre-configured, optimized images built following the approach described below, which are pre-built and maintained by the Kairos team.
{{% /alert %}}

Kairos is a meta-distribution as, components of Kairos can be used to build, Immutable, container-based derivatives.

Kairos packages can be used from standard Dockerfiles to build full-fledged Kairos-based derivatives entirely from container images. 

Consider this example which builds an alpine based derivative:

```Dockerfile
FROM quay.io/luet/base as luet
FROM alpine

# Install packages in the image. In a systemd-based system you would install systemd instead of openrc.
# Note, grub2 is a required dependency, along as an init system (currently openRC and systemd are supported)
RUN apk --no-cache add  \
      grub \
      grub-efi \
      grub-bios \
      bash \
      bonding \
      bridge \
      connman \
      gettext \
      squashfs-tools \
      openrc \
      parted \
      e2fsprogs \
      logrotate \
      dosfstools \
      coreutils \
      which \
      curl \
      nano \
      gawk \
      haveged \
      tar \
      rsync \
      bash-completion \
      blkid \
      busybox-openrc \
      ca-certificates \
      conntrack-tools \
      coreutils \
      cryptsetup \
      curl \
      dbus \
      dmidecode \
      dosfstools \
      e2fsprogs \
      e2fsprogs-extra \
      efibootmgr \
      eudev \
      fail2ban \
      findutils \
      gcompat \
      grub-efi \
      haveged \
      htop \
      hvtools \
      iproute2 \
      iptables \
      irqbalance \
      iscsi-scst \
      jq \
      kbd-bkeymaps \
      lm-sensors \
      libc6-compat \
      libusb \
      logrotate \
      lsscsi \
      lvm2 \
      lvm2-extra \
      mdadm \
      mdadm-misc \
      mdadm-udev \
      multipath-tools \
      ncurses \
      ncurses-terminfo \
      nfs-utils \
      open-iscsi \
      rbd-nbd \
      openrc \
      openssh-client \
      openssh-server \
      parted \
      procps \
      qemu-guest-agent \
      rng-tools \
      rsync \
      strace \
      smartmontools \
      sudo \
      tar \
      tzdata \
      util-linux \
      vim \
      wireguard-tools \
      wpa_supplicant \
      xfsprogs \
      xz \
      open-vm-tools \
      open-vm-tools-deploypkg \
      open-vm-tools-guestinfo \
      open-vm-tools-static \
      open-vm-tools-vmbackup

# Enable some services
RUN rc-update add sshd boot && \
    rc-update add connman boot  && \
    rc-update add acpid boot && \
    rc-update add hwclock boot && \
    rc-update add syslog boot && \
    rc-update add udev sysinit && \
    rc-update add udev-trigger sysinit && \
    rc-update add ntpd boot && \
    rc-update add crond && \
    rc-update add fail2ban

# Symlinks to make boot-assessment work
RUN ln -s /usr/sbin/grub-install /usr/sbin/grub2-install && \
    ln -s /usr/bin/grub-editenv /usr/bin/grub2-editenv

# Now we install Kairos dependencies below
COPY --from=luet /usr/bin/luet /usr/bin/luet

RUN mkdir -p /etc/luet/repos.conf.d && \
    luet repo add kairos -y --url quay.io/kairos/packages --type docker

RUN luet install -y \
        system/base-cloud-config \
        dracut/immutable-rootfs \
        dracut/network \
        static/grub-config \
        system/suc-upgrade \
        system/shim \
        system/grub2-efi \
        system/elemental-cli \
        init-svc/openrc
        # use init-svc/systemd for systemd based distros

# Install kernels from the Kairos repositories, or regenerate the initrd with dracut
RUN luet install -y distro-kernels/opensuse-leap distro-initrd/opensuse-leap

# Enable services
    RUN mkdir -p /etc/runlevels/default && \
    ln -sf /etc/init.d/cos-setup-boot /etc/runlevels/default/cos-setup-boot  && \
    ln -sf /etc/init.d/cos-setup-network /etc/runlevels/default/cos-setup-network  && \
    ln -sf /etc/init.d/cos-setup-reconcile /etc/runlevels/default/cos-setup-reconcile && \
    ln -sf /etc/init.d/kairos-agent /etc/runlevels/default/kairos-agent

# On systemd would be:
	#RUN systemctl enable cos-setup-rootfs.service && \
	#    systemctl enable cos-setup-initramfs.service && \
	#    systemctl enable cos-setup-reconcile.timer && \
	#    systemctl enable cos-setup-fs.service && \
	#    systemctl enable cos-setup-boot.service && \
	#    systemctl enable cos-setup-network.service

# Optionally, put your fixed cloud config files in here following our docs https://kairos.io/docs/reference/configuration/
# RUN cp cloud.yaml /system/oem/configuration.yaml 
```

After building the container image, we can create an ISO:

{{< tabpane text=true  >}}
{{% tab header="AuroraBoot" %}}

We can use [AuroraBoot](/docs/reference/auroraboot) to handle the the ISO build process and optionally attach it a default cloud config, for example:

```bash
$ IMAGE=<source/image>
$ docker run -v $PWD/cloud_init.yaml:/cloud_init.yaml \
                    -v $PWD/build:/tmp/auroraboot \
                    -v /var/run/docker.sock:/var/run/docker.sock \
                    --rm -ti quay.io/kairos/auroraboot \
                    --set container_image=docker://$IMAGE \
                    --set "disable_http_server=true" \
                    --set "disable_netboot=true" \
                    --cloud-config /cloud_init.yaml \
                    --set "state_dir=/tmp/auroraboot"
# Artifacts are under build/
$ sudo ls -liah build/iso
total 778M
34648528 drwx------ 2 root root 4.0K Feb  8 16:39 .
34648526 drwxr-xr-x 5 root root 4.0K Feb  8 16:38 ..
34648529 -rw-r--r-- 1 root root  253 Feb  8 16:38 config.yaml
34649370 -rw-r--r-- 1 root root 389M Feb  8 16:38 kairos.iso
34649372 -rw-r--r-- 1 root root 389M Feb  8 16:39 kairos.iso.custom.iso
34649371 -rw-r--r-- 1 root root   76 Feb  8 16:39 kairos.iso.sha256
```
{{% /tab %}}
{{% tab header="Manually" %}}
Use `osbuilder-tools`:
```bash
docker run -v $PWD:/cOS -v /var/run/docker.sock:/var/run/docker.sock -i --rm quay.io/kairos/osbuilder-tools:latest --name "custom-iso" --debug build-iso --date=false --local $IMAGE --output /cOS/
```
{{% /tab %}}
{{< /tabpane >}}

Use QEMU to test the ISO:

```bash
qemu-system-x86_64 -m 2048 -drive if=virtio,media=disk,file=custom-iso.iso
```
