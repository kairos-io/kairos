# Jetson Thor QSPI Firmware Upgrade Support

**Date:** 2026-07-20
**Status:** Design — approved, pending implementation plan
**Tracking issue:** [kairos-io/kairos#4228](https://github.com/kairos-io/kairos/issues/4228)

## Problem

A Jetson AGX Thor flashed with an upstream Kairos image black-screens on boot when the
board's QSPI boot firmware version does not match the L4T version baked into the image.

`kairos-init` currently pins Thor to `L4T_VERSION 38.4` (`pkg/stages/steps_install.go:82`)
while boards ship with, or get updated to, other versions. Reporter of #4228 hit this with
a board at QSPI 39.2 against a 38.4 image, and updating the firmware to 39.2 did not help —
the versions must correspond.

Kairos ships the machinery to fix this and never uses it. `kairos-init` installs
`nvidia-l4t-bootloader`, which carries a ~29 MB UEFI capsule payload, but also creates
NVIDIA's opt-out sentinel at `pkg/stages/steps_install.go:189`:

```
touch /opt/nvidia/l4t-packages/.nv-l4t-disable-boot-fw-update-in-preinstall
```

That sentinel is correct for a container build — you cannot flash a device's QSPI from
inside Docker — but nothing ever performs the equivalent step on real hardware. The capsule
sits unused in every Jetson Kairos image.

## Background: how the NVIDIA installer actually does it

Findings from analysing `jetsoninstaller-r39.2.0-2026-06-01-23-53-13-arm64.iso`.

### It is UEFI capsule update, exclusively

r39.2 (JetPack 7) uses UEFI capsule update only. The older `nv_update_engine` +
`bl_update_payload` flow is dead code: the binaries still ship in `nvidia-l4t-tools` and
`nvidia-l4t-bootloader-utils` and still default to `/opt/ota_package/bl_update_payload`, but
no such payload exists in the package and nothing invokes them. Most Jetson documentation
found online describes that obsolete flow.

### There is no user prompt

Contrary to initial assumption, the installer never asks the user anything. The decision is
a pure version comparison. Verified negatives: subiquity snap 6808 is stock (zero hits for
`qspi|nvbootctrl|capsule|nv_update_engine`), as are `/casper/initrd` and all six squashfs
layers. `nvidia-l4t-bootloader` ships no debconf templates.

Any on-screen firmware message is this progress line, not a question:

```
logger -p info "[...] 67% Step 9/13 Updating boot firmware"
```

### It runs late in install, not early in boot

The trigger lives in `late-commands` of `nv-ai/ai/jetsoniso_thor-ai.yaml` (line ~528).
There are no `early-commands`. The arming step is removal of the sentinel, followed by a
package reinstall inside the target chroot:

```yaml
- rm -f /target/opt/nvidia/l4t-packages/.nv-l4t-disable-boot-fw-update-in-preinstall
- curtin in-target -- /bin/bash -c "unset SNAP; DEBIAN_FRONTEND=noninteractive \
    apt-get install --reinstall -y --no-install-recommends nvidia-l4t-bootloader"
```

Nothing is written to QSPI at that moment. The postinst only stages a capsule on the ESP;
**UEFI performs the actual flash on the next boot, before Linux starts.** One reboot.

Note the reinstall runs in the target chroot but reads `/proc/cmdline` and
`/sys/firmware/efi/*` from the live installer environment — same physical QSPI, and the
`autoinstall` cmdline token must be visible. This is why the playbook bind-mounts `/dev`.

### Version detection

Current QSPI version, decimal-encoded as `(branch<<16)|(major<<8)|minor`:

```bash
cat /sys/firmware/efi/esrt/entries/entry0/fw_version   # 2556416 == 0x270200 == 39.2.0
```

Fallback is `/sys/class/dmi/id/bios_version` (format `bsp_ver-gcid-NNNNNNNN`), used by the
shared utility but not by the ISO path, which hard-fails if ESRT is absent.

Expected version has no version file — it is the dpkg version of `nvidia-l4t-bootloader`
itself (`39.2.0-20260601141651` → `39.2.0` → `2556416`).

### The comparison is one-directional

```bash
if [ $((current_pkg_ver_converted)) -le $((current_qspi_ver)) ]; then
    echo "INFO. No need to trigger capsule update."; return 0
fi
```

A capsule is staged only when the package is **newer** than the QSPI. A board whose QSPI is
newer than the image is a silent no-op. NVIDIA's slot-sync code confirms this is deliberate:
`not-support: current slot version is older, not support rollback`.

**Consequence: porting NVIDIA's mechanism verbatim would not have fixed #4228.** It covers
only the factory-board-older-than-image direction.

There is also a hard floor: `factory_qspi_ver="2490368"` (38.0.0). Below it the ISO refuses
and the board requires a USB host flash.

### The staging action reduces to two operations

`nv_bootloader_capsule_updater.sh -q <payload>` does exactly:

```bash
cp "${payload_file}" "${capsule_dir}"          # /boot/efi/EFI/UpdateCapsule/
printf "\x07\x00\x00\x00\x04\x00\x00\x00\x00\x00\x00\x00" > "${TMP_FILE}"
dd if="${TMP_FILE}" of="${var_path}"
# var_path = /sys/firmware/efi/efivars/OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c
```

(`\x07` = NV+BS+RT attributes; `\x04` = bit 2, `EFI_OS_INDICATIONS_FILE_CAPSULE_DELIVERY_SUPPORTED`.)

The Thor payload is `/opt/ota_package/t26x/TEGRA_BL_3834_agx.Cap` (29,378,612 bytes), a
standard UEFI FMP capsule. Directory by chip id from `/etc/nv_boot_control.conf`
(`0x23`→`t23x`, `0x26`→`t26x`); file by board id from `COMPATIBLE_SPEC` — for Thor,
`select_3834_payload()` is unconditional.

Beyond staging, the postinst also runs `copy_l4tlauncher` and `extlinux_update`. **Both are
harmful to Kairos** — see Non-goals.

### Thor gets no automatic boot-time update

`auto_update_qspi()` in `nv-l4t-bootloader-update.sh` dispatches only on
`jetson-orin-nano-devkit*` (SKU 0005) and the IGX boards. `jetson-agx-thor-devkit` has no
branch, so the every-boot `nv-l4t-bootloader-config.service` path is a no-op on Thor. The
deb postinst is the only trigger that exists.

## Design

### Decisions taken

| Question | Decision |
|---|---|
| When does it run | Install time only |
| User interaction | Fully automatic, no prompt (matches NVIDIA) |
| Newer-QSPI-than-image | Abort the install, loudly |
| Where the code lives | `kairos-init` cloud-config stage |

### Component 1 — cloud-config stage

New `13_nvidia_qspi.yaml` in `kairos-init`'s `pkg/bundled/cloudconfigs/`, following the
existing `12_nvidia.yaml` pattern, running on the `after-install-chroot` stage.
`kairos-agent` already exposes this hook (`pkg/action/install.go:189`,
`AfterInstallChrootHook` in `pkg/constants/constants.go:66`).

Gate: Jetson model check (as in `12_nvidia.yaml`) plus chip id `0x26` read from
`/etc/nv_boot_control.conf`.

### Component 2 — decision logic

Read current from ESRT, expected from `dpkg-query -W -f='${Version}' nvidia-l4t-bootloader`
in the target rootfs. Encode both as `(branch<<16)|(major<<8)|minor`.

| Condition | Action |
|---|---|
| ESRT unreadable | Abort — cannot determine firmware state |
| QSPI < 38.0.0 (`2490368`) | Abort, instruct USB host flash |
| image L4T > QSPI | Stage capsule, proceed |
| image L4T == QSPI | No-op, proceed |
| image L4T < QSPI | Abort, naming both versions |

The final row is a deliberate superset of NVIDIA's behaviour and is the fix for #4228.
Aborting is preferred over warn-and-continue: the alternative outcome is a black screen with
no diagnostic, which is precisely the reported failure. Error messages must state both
versions and the remedy.

### Component 3 — staging action

Reimplement the two load-bearing operations directly against the target ESP. Do **not**
invoke `dpkg-reconfigure` or re-arm the sentinel.

```bash
mkdir -p <esp>/EFI/UpdateCapsule/
cp /opt/ota_package/t26x/TEGRA_BL_3834_agx.Cap <esp>/EFI/UpdateCapsule/
printf "\x07\x00\x00\x00\x04\x00\x00\x00\x00\x00\x00\x00" \
  > /sys/firmware/efi/efivars/OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c
```

The sentinel at `steps_install.go:189` stays exactly as-is.

### Component 4 — version policy

Bump the Thor L4T pin from `38.4` to `39.2` in `pkg/stages/steps_install.go:82`. Keeping
this current is an ongoing obligation: until the image catches up, the abort-on-newer path
is a dead end for users on newer hardware.

## Non-goals

- **Do not call `copy_l4tlauncher`.** It overwrites `/boot/efi/EFI/BOOT/BOOTAA64.efi` with
  L4TLauncher. On Kairos that path is our own bootloader; doing this would break boot.
- **Do not call `extlinux_update`.** It rewrites `/boot/extlinux/extlinux.conf` with a
  `root=` taken from `/proc/cmdline`. Meaningless and harmful under Kairos's boot flow.
- **Do not attempt firmware downgrade.** NVIDIA marks rollback unsupported; attempting it
  risks bricking a board.
- **No upgrade-time or boot-time path in this iteration.** Install-time only. An upgrade
  hook may follow once the install path is proven on hardware.
- **No interactive confirmation.** Matches NVIDIA and suits unattended edge deployments.

## Open questions requiring hardware validation

These could not be determined from the ISO and must be answered on a real Thor before
implementation is considered complete.

1. **ESP capacity.** Kairos defaults to a 64 MiB ESP (`kairos-sdk` `constants.EfiSize`); the
   capsule is 29 MB. Tight once GRUB and shim are present. NVIDIA's
   `RequireESPFreeSpace=0x3C00000` (60 MB) is an fwupd setting we bypass, so the real
   requirement is ~29 MB free — but this must be measured, not assumed. May force a larger
   ESP for Thor. Note the UKI/trusted-boot path uses `ImgSize * 5` and is unaffected.
2. **Overlayfs guard.** NVIDIA's postinst bails when the `L4TOverlayFsMode` efivar
   (`360a04b9-3fe6-42c8-9fad-d9bf459c4770`) is `0700000001000000`. We bypass the postinst,
   so it should not block us — unless UEFI itself honours that variable when applying
   capsules. Unverified.
3. **`/etc/nv_boot_control.conf` presence.** Created at flash time, shipped by no package,
   and load-bearing for chip and board detection. Must confirm it exists on a
   Kairos-installed Thor.
4. **ESP writability and efivars access** at `after-install-chroot` time.

## Testing

- Unit-level: version encode/decode (`38.4.0` → `2491392`, `39.2.0` → `2556416`) and the
  full decision table, including both abort paths.
- Hardware: a Thor at QSPI 38.0.0 with a 39.2 image must stage and flash in one reboot; a
  Thor at 39.2 with a 38.4 image must abort with a legible message rather than black-screen.
- Regression: AGX Orin and Orin NX images must be unaffected — the stage is Thor-gated.

## References

- Issue: https://github.com/kairos-io/kairos/issues/4228
- Prior art worth reviewing before implementation, not yet assessed:
  https://github.com/anduril/jetpack-nixos#updating-firmware-from-device
- Canonical QSPI flash docs:
  https://canonical-ubuntu-for-jetson.readthedocs-hosted.com/latest/how-to/flash/#qspi-for-jetson-agx-thor
- Analysed artifact: `jetsoninstaller-r39.2.0-2026-06-01-23-53-13-arm64.iso`
