# kairos-init

> kairos-init lives in the Kairos monorepo. See the
> [root README](../README.md) for the full repository layout. The
> published `quay.io/kairos/kairos-init` image is built from this
> directory and embeds the multi-call `kairos` binary shipped from
> `cmd/kairos/`.

kairos-init is an initializer for container images to be Kairosified.

You only need to run this once inside a Dockerfile to have a system that has all the necessary tools to run Kairos.

## Quick example

Create a Dockerfile with your desired base image, mount the kairos-init binary from the kairos-init image and run it:

```Dockerfile
FROM quay.io/kairos/kairos-init:latest AS kairos-init

FROM ubuntu:24.04
ARG VERSION=1.0.0
RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init /kairos-init --version "${VERSION}"
```

Then build it:

```bash
docker build -t my-kairosified-image .
```

You can then use [AuroraBoot](https://github.com/kairos-io/auroraboot) to transform that image into an ISO, RAW image, or use it as an upgrade source for a running Kairos system.

## Dry-run

Pass `--dry-run` to print the resolved yip stages that would run without
executing them (no `yip` execution, file copies, or provider hook runs). The console logger is silenced so
stdout is the raw yip YAML — the same format `yip` itself consumes, so
you can pipe it back into `yip` (or diff it across builds) directly:

```bash
kairos-init --dry-run --version 1.0.0 > preview.yaml
kairos-init --dry-run --version 1.0.0 --stage init | yip -
```

The output is a full `schema.YipConfig` (commands, files, packages,
services, etc.). Nothing is written to `/etc/kairos` and no binary copy,
`yip` execution, or provider hook runs during a dry run.

## CIS L1 Hardening

kairos-init applies a subset of the CIS Distribution Independent Linux
v2.0.0 L1 controls to every image it builds. This is not full L1 coverage —
SELinux enforcing mode, sysctl hardening, auditd, the PAM/login.defs controls
and the local `/etc/issue` banner are not touched:

- `/etc/modprobe.d/cis-blocklist.conf` gets an `install <mod> /bin/false` line
  for cramfs, freevxfs, jffs2, hfs, hfsplus and udf (sections 1.1.1.1-1.1.1.6).
  `modprobe` of those filesystems fails, so volumes that need them no longer
  mount.
- `/etc/issue.net` is replaced with a generic pre-authentication warning that
  names no distribution, release or kernel (section 1.7). Wiring sshd to print
  it (`Banner /etc/issue.net`) is not done here — set that yourself if you want
  the banner on ssh logins.
- `/etc/passwd`, `/etc/group`, `/etc/shadow`, `/etc/gshadow` and their `-`
  backups have their modes tightened (section 6.1). Only the shadow files and
  the backups lose read access; `/etc/passwd` and `/etc/group` stay
  world-readable, and ownership is left as the base image shipped it.

Pass `--skip-step cisHardening` to turn the whole set off.

## NVIDIA / Jetson

### Jetson AGX Thor QSPI firmware

Thor boards will not boot if the QSPI boot firmware version does not correspond to the L4T
version in the image. Kairos images pin L4T `39.2`.

At install time, `after-install-chroot` runs `/usr/sbin/kairos-jetson-qspi-update` on boards
whose devicetree SoC compatible string is `nvidia,tegra264` (uniform across every Thor
variant: AGX, IGX and devkit). It compares the board's firmware version (from the UEFI ESRT)
against the image's `nvidia-l4t-bootloader` version and:

- stages a UEFI capsule update when the image is newer — applied by UEFI on the next boot;
- does nothing when they match;
- **aborts the install** when the board firmware is newer than the image, because UEFI
  capsule update cannot downgrade firmware. Use a Kairos image matching the board, or
  reflash the board;
- **aborts the install** when the board is below L4T 38.0.0, which needs a USB host flash.

Override the L4T version with the `L4T_VERSION` environment variable.

See [kairos-io/kairos#4228](https://github.com/kairos-io/kairos/issues/4228).

## Documentation

For full documentation — including all available flags, configuration options, examples, extending stages, building for Trusted Boot, RHEL images, and more — please refer to the official Kairos documentation:

**[https://kairos.io/docs/reference/kairos-factory/](https://kairos.io/docs/reference/kairos-factory/)**
