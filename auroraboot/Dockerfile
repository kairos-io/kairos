ARG FEDORA_VERSION=44
ARG LUET_VERSION=0.37.0
ARG SWAGGER_STAGE=with-swagger
ARG TARGETARCH

FROM quay.io/luet/base:$LUET_VERSION AS luet

# Build the React UI. Output is platform-independent.
FROM node:26 AS js
WORKDIR /work/ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm install
COPY ui/ .
RUN npm run build

FROM fedora:$FEDORA_VERSION AS base
ARG TARGETARCH
ENV BUILDKIT_PROGRESS=plain
ENV LUET_NOLOCK=true
ENV TMPDIR=/tmp
# `luet repo update` fails with `/usr/bin/unpigz: invalid argument` on arm for some reason without this option:
# https://github.com/containerd/containerd/blob/7c3aca7a610df76212171d200ca3811ff6096eb8/archive/compression/compression.go#L50
ENV CONTAINERD_DISABLE_PIGZ=1
# Mirrors drop a request now and then, and a single 404 used to end the build.
# scripts/retry runs a mirror fetch again if it fails, so one dropped
# request no longer ends the build. See kairos-io/kairos#5565.
COPY scripts/retry /usr/local/bin/retry
RUN retry dnf -y update
RUN retry dnf -y install dnf-plugins-core && retry dnf-3 config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo
## ISO+ Arm image + Netboot + cloud images Build depedencies
# opensc is needed for the pkcs11 module to work
# llvm is needed for uki building specifically llvm-objcopy
RUN retry dnf in -y bc \
              binutils \
              busybox \
              containerd.io \
              curl \
              docker-ce \
              docker-ce-cli \
              docker-buildx-plugin \
              dosfstools \
              e2fsprogs \
              erofs-utils \
              gdisk \
              genisoimage \
              grub2 \
              jq \
              kpartx \
              lvm2 \
              llvm \
              mtools \
              openssl \
              opensc \
              parted \
              rsync \
              sbsigntools \
              squashfs-tools \
              sudo \
              udev \
              util-linux \
              xorriso \
              zstd


FROM golang:1.27 AS with-swagger
WORKDIR /app
# scripts/retry runs a network fetch again if it fails, so one dropped
# request no longer ends the build. See kairos-io/kairos#5565.
COPY scripts/retry /usr/local/bin/retry
# Build the swag CLI from the version go.mod pins through its tool directive.
# "go install ...@latest" fetched an unpinned CLI on every build, so the
# generated docs could change with no code change, and a proxy.golang.org
# hiccup failed the whole image build.
COPY go.mod go.sum ./
RUN retry go build -o /usr/local/bin/swag github.com/swaggo/swag/cmd/swag
COPY . .
RUN swag init -g internal/cmd/web.go --output docs --parseDependency --parseInternal --parseDepth 2

FROM golang:1.27 AS without-swagger
WORKDIR /app

FROM ${SWAGGER_STAGE} AS swagger

FROM golang:1.27 AS builder
ARG VERSION=v0.0.0
WORKDIR /work
# libpcsclite-dev is required by github.com/go-piv/piv-go/v2 (transitive via sbctl)
RUN apt-get update && apt-get install -y --no-install-recommends libpcsclite-dev && rm -rf /var/lib/apt/lists/*
COPY scripts/retry /usr/local/bin/retry
ADD go.mod .
ADD go.sum .
RUN retry go mod download
ADD . .
COPY --from=js /work/internal/ui/dist ./internal/ui/dist
COPY --from=swagger /app/docs ./docs
ENV VERSION=$VERSION
RUN go build -tags ui -ldflags "-X main.version=${VERSION}" -o auroraboot


# RISC-V 64 stage - uses fedorariscv/base since official fedora:42 lacks riscv64
FROM fedorariscv/base:45 AS riscv64
ENV BUILDKIT_PROGRESS=plain
ENV TMPDIR=/tmp
# scripts/retry runs a mirror fetch again if it fails, so one dropped
# request no longer ends the build. See kairos-io/kairos#5565.
COPY scripts/retry /usr/local/bin/retry
# The base image ships no usable repo config and Fedora has no riscv64 mirrors,
# so use the Fedora RISC-V Koji repo and turn the stock Fedora repos off
RUN printf '%s\n' '[fedora-riscv-koji]' 'name=Fedora RISC-V Koji' \
        'baseurl=https://openkoji.iscas.ac.cn/kojifiles/repos/f$releasever-build/latest/riscv64/' \
        'enabled=1' 'gpgcheck=0' > /etc/yum.repos.d/openkoji-fedora.repo && \
    printf '[fedora]\nenabled=0\n' > /etc/yum.repos.d/fedora.repo && \
    printf '[updates]\nenabled=0\n' > /etc/yum.repos.d/fedora-updates.repo && \
    printf '[updates-testing]\nenabled=0\n' > /etc/yum.repos.d/fedora-updates-testing.repo && \
    printf '[fedora-cisco-openh264]\nenabled=0\n' > /etc/yum.repos.d/fedora-cisco-openh264.repo
# Install base dependencies (same as main base stage)
RUN retry dnf -y update && \
    retry dnf -y install dnf-plugins-core && \
    retry dnf in -y bc binutils curl dosfstools e2fsprogs erofs-utils gdisk genisoimage \
              grub2 jq kpartx lvm2 llvm mtools openssl parted rsync squashfs-tools \
              sudo udev util-linux xorriso zstd

# The EFI packages are optional, so only their install may fail. Keep it in a
# RUN of its own: a trailing || true on the step above would also hide a failed
# update or base install. See kairos-io/kairos#5823.
RUN retry dnf install -y \
        systemd-boot-unsigned \
        grub2-efi-riscv64 \
        grub2-efi-riscv64-modules || true

# Set up systemd-boot artifacts in the expected location for UKI building
# The code expects files at /riscv64/systemd-boot/linuxriscv64.efi.stub and systemd-bootriscv64.efi
RUN mkdir -p /riscv64/systemd-boot && \
    cp /usr/lib/systemd/boot/efi/linuxriscv64.efi.stub /riscv64/systemd-boot/ 2>/dev/null || true && \
    cp /usr/lib/systemd/boot/efi/systemd-bootriscv64.efi /riscv64/systemd-boot/ 2>/dev/null || true

# Set up grub artifacts for riscv64 ISO/raw image building
RUN mkdir -p /riscv64/raw/grubartifacts/EFI/BOOT && \
    cp /usr/lib/grub/riscv64-efi/grub.efi /riscv64/raw/grubartifacts/EFI/BOOT/grub.efi 2>/dev/null || \
    grub2-mkimage -O riscv64-efi -o /riscv64/raw/grubartifacts/EFI/BOOT/grub.efi -p /boot/grub2 \
        part_gpt part_msdos fat ext2 iso9660 linux boot chain configfile normal search search_label search_fs_file search_fs_uuid ls || true

COPY --from=builder /work/auroraboot /usr/bin/auroraboot

ENTRYPOINT ["/usr/bin/auroraboot"]

# Luet-based stage for amd64/arm64
FROM base AS luet-base
COPY --from=luet /usr/bin/luet /usr/bin/luet
# copy both arches
COPY image-assets/luet-arm64.yaml /tmp/luet-arm64.yaml
COPY image-assets/luet-amd64.yaml /tmp/luet-amd64.yaml
# Set the default luet config to the current build arch
RUN mkdir -p /etc/luet/
RUN cp /tmp/luet-${TARGETARCH}.yaml /etc/luet/luet.yaml
## Uki artifacts, will be set under the /usr/kairos directory
RUN luet repo update
## Each arch has its own systemd-boot artifacts, we should ship both in both images for multi-arch build support
RUN luet install --config /tmp/luet-arm64.yaml -y system/systemd-boot --system-target /arm/systemd-boot
RUN luet install --config /tmp/luet-amd64.yaml -y system/systemd-boot --system-target /amd/systemd-boot

## RPI64
## Both arches have the same package files so no matter the arch here.
RUN luet install -y firmware/u-boot-rpi64 firmware/rpi --system-target /arm/rpi/

## PineBook64 Pro
RUN luet install -y uboot/rockchip --system-target /arm/pinebookpro/

## Odroid fw
RUN luet install -y firmware/odroid-c2 --system-target /arm/odroid-c2

## RAW images for arm64

# Orin uses these artifacts
RUN luet install --config /tmp/luet-arm64.yaml -y static/grub-efi --system-target /arm/raw/grubefi
RUN luet install --config /tmp/luet-arm64.yaml -y static/grub-config --system-target /arm/raw/grubconfig
# Orin uses these artifacts. Alpine uses these artifacts for fallback efi values
RUN luet install --config /tmp/luet-arm64.yaml -y static/grub-artifacts --system-target /arm/raw/grubartifacts
# You can build amd64 raw images for alpine so....we need this in both images
RUN luet install --config /tmp/luet-amd64.yaml -y static/grub-artifacts --system-target /amd/raw/grubartifacts

# remove luet tmp files. Side effect of setting the system-target is that it treats it as a root fs
# so temporal files are stored in each dir
RUN rm -Rf /arm/systemd-boot/var/tmp
RUN rm -Rf /amd/systemd-boot/var/tmp
RUN rm -Rf /arm/systemd-boot/var/cache
RUN rm -Rf /amd/systemd-boot/var/cache
RUN rm -Rf /arm/rpi/var/tmp
RUN rm -Rf /arm/rpi/var/cache
RUN rm -Rf /arm/pinebookpro/var/tmp
RUN rm -Rf /arm/pinebookpro/var/cache
RUN rm -Rf /arm/odroid-c2/var/tmp
RUN rm -Rf /arm/odroid-c2/var/cache
RUN rm -Rf /arm/raw/grubconfig/var/tmp
RUN rm -Rf /arm/raw/grubconfig/var/cache
RUN rm -Rf /arm/raw/grubartifacts/var/tmp
RUN rm -Rf /amd/raw/grubartifacts/var/tmp
RUN rm -Rf /arm/raw/grubartifacts/var/cache
RUN rm -Rf /amd/raw/grubartifacts/var/cache
RUN rm -Rf /arm/raw/grubefi/var/tmp
RUN rm -Rf /arm/raw/grubefi/var/cache
# Remove the var dir if empty
RUN rm -d /arm/systemd-boot/var || true
RUN rm -d /amd/systemd-boot/var || true
RUN rm -d /arm/rpi/var || true
RUN rm -d /arm/pinebookpro/var || true
RUN rm -d /arm/odroid-c2/var || true
RUN rm -d /arm/raw/grubconfig/var || true
RUN rm -d /arm/raw/grubartifacts/var || true
RUN rm -d /amd/raw/grubartifacts/var || true
RUN rm -d /arm/raw/grubefi/var || true

COPY --from=builder /work/auroraboot /usr/bin/auroraboot

ENTRYPOINT ["/usr/bin/auroraboot"]

# Architecture aliases - amd64/arm64 use luet-base, riscv64 is defined above
FROM luet-base AS amd64
FROM luet-base AS arm64

# Final stage - dynamically selects based on TARGETARCH
FROM ${TARGETARCH} AS default
