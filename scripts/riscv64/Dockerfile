# Lean core-only image for manual riscv64 testing (no k8s, trusted boot, or FIPS).

ARG BASE_IMAGE=ubuntu:24.04
ARG KAIROS_INIT_IMAGE=quay.io/kairos/kairos-init:v0.15.2
ARG MODEL=generic
ARG VERSION=v0.0.1
ARG DISTRO=ubuntu

FROM ${KAIROS_INIT_IMAGE} AS kairos-init

FROM ${BASE_IMAGE}
ARG MODEL
ARG VERSION
ARG DISTRO

# systemctl segfaults under QEMU user emulation during docker build on riscv64.
RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    ARCH=$(uname -m); \
    if [ "$ARCH" = "riscv64" ]; then SKIP_SERVICES="--skip-step services"; else SKIP_SERVICES=""; fi; \
    /kairos-init -l debug -s install -m "${MODEL}" --version "${VERSION}" ${SKIP_SERVICES} && \
    /kairos-init -l debug -s init -m "${MODEL}" --version "${VERSION}" ${SKIP_SERVICES}

# Workaround until kairos-init#398 is released: Debian-family images still need
# explicit grub-efi-riscv64-bin and the monolithic symlink for AuroraBoot v0.22.0.
RUN if [ "${DISTRO}" = "ubuntu" ] || [ "${DISTRO}" = "debian" ]; then \
      apt-get update && \
      DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends grub-efi-riscv64-bin && \
      mkdir -p /usr/lib/grub/riscv64-efi && \
      if [ -f /usr/lib/grub/riscv64-efi/monolithic/grubriscv64.efi ] && \
         [ ! -e /usr/lib/grub/riscv64-efi/grubriscv64.efi ]; then \
        ln -sf monolithic/grubriscv64.efi /usr/lib/grub/riscv64-efi/grubriscv64.efi; \
      fi && \
      rm -rf /var/lib/apt/lists/*; \
    fi

# Debian riscv64 ships vmlinux-* (not vmlinuz-*). kairos-init <=v0.15.2 used an
# absolute /boot/vmlinuz symlink that AuroraBoot cannot resolve inside the ISO
# rootfs; use a relative link so build-iso finds the kernel.
RUN if [ "${DISTRO}" = "debian" ]; then \
      for k in /boot/vmlinux-*; do \
        [ -f "$k" ] || continue; \
        ln -sf "$(basename "$k")" /boot/vmlinuz; \
        break; \
      done; \
    fi

# openSUSE ships grub at /usr/share/efi/riscv64/grub.efi; kairos-agent/sdk search
# paths still include the Debian-style name. Symlink so EFI install finds grub.
RUN if [ "${DISTRO}" = "opensuse" ]; then \
      if [ -f /usr/share/efi/riscv64/grub.efi ] && \
         [ ! -e /usr/lib/grub/riscv64-efi/grubriscv64.efi ]; then \
        mkdir -p /usr/lib/grub/riscv64-efi && \
        ln -sf /usr/share/efi/riscv64/grub.efi /usr/lib/grub/riscv64-efi/grubriscv64.efi; \
      fi; \
    fi
