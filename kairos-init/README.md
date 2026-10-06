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
FROM ubuntu:24.04
ARG VERSION=1.0.0
RUN --mount=type=bind,from=quay.io/kairos/kairos-init:v4.3.0,src=/kairos-init,dst=/kairos-init /kairos-init --version "${VERSION}"
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

kairos-init applies the CIS Distribution Independent Linux v2.0.0 L1 controls
that fit an immutable image to every image it builds. `tests/cis_test.go` runs
the [cis-dil-benchmark](https://github.com/dev-sec/cis-dil-benchmark) profile
against an installed Hadron system in CI; the controls that do not apply, or
were left out on purpose, are listed with the reason in
`tests/assets/cis-dil-waivers.yaml`.

What the `cisHardening` step sets:

- `/etc/modprobe.d/cis-blocklist.conf` gets an `install <mod> /bin/false` line
  for cramfs, freevxfs, jffs2, hfs, hfsplus and udf (1.1.1.x). `modprobe` of
  those filesystems fails, so volumes that need them no longer mount.
- `/etc/issue.net` is replaced with a generic pre-authentication warning that
  names no distribution, release or kernel (1.7). Wiring sshd to print it
  (`Banner /etc/issue.net`) is not done here.
- No core dumps: `* hard core 0` in `/etc/security/limits.d/50-kairos-cis.conf`
  and `fs.suid_dumpable = 0` (1.5.1).
- Network sysctls in `/etc/sysctl.d/99-kairos-cis.conf`: ASLR, loose
  `rp_filter`, SYN cookies, no source routes, no (secure) redirects, no IPv6
  router advertisements (section 3).
- auditd enabled with the baseline rules in `/etc/audit/rules.d/50-kairos.rules`
  (4.1).
- journald compresses and keeps its journal on disk (4.2.2.2, 4.2.2.3), and
  files under `/var/log` lose group write and every bit for other (4.2.3).
- cron and at paths are tightened on bases that ship them (5.1).
- `pwquality.conf` and `faillock.conf`, wired into PAM per distro (5.4.1,
  5.4.2). Hadron ships no `pam_pwquality.so`, so the password quality policy
  does nothing there yet. On Hadron, `pam_unix` remembers the last 5
  passwords (5.3.3).
- `/etc/login.defs` aging and `UMASK` are only ever tightened, and the shell
  startup files get a matching `umask` (5.4.4).
- `su` is limited to the `wheel` group (5.6). Root and `sudo` are not affected.
- `/etc/gshadow` and the `-` backups are created if missing, and the account
  databases get their modes tightened (6.1). The dbus launch helper gets its
  `messagebus` group when the base ships it ungrouped (6.1.12).
- `/system/oem/35_cis_boot.yaml` runs at every boot: it creates
  `/usr/local/sbin` (6.2.6, `/usr/local` is the persistent partition) and
  closes home directories to other users (6.2.8).

immucore mounts `/tmp` nodev,nosuid,noexec, remounts `/dev/shm` noexec and
mounts the `/home` bind nodev (1.1.3-1.1.5, 1.1.14, 1.1.17).

Not covered: SELinux enforcing, firewall policy, password aging on users
provisioned from cloud-config, a GRUB password, and blocking usb-storage.

Pass `--skip-step cisHardening` to turn off the kairos-init part. The immucore
mount options always apply.

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
