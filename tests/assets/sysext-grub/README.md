Sysext test extensions for the plain GRUB live-media sweep.

This directory is baked onto every non-UKI test ISO by `_build-iso.yaml`
via auroraboot's `--overlay-iso`. One extension ships:

- `work.sysext.raw`: verity, **unsigned**. The GRUB drop-in installed
  by `kairos-init` runs `systemd-sysext refresh` with
  `--image-policy="root=verity+absent:usr=verity+absent"`, which
  accepts a verity-protected image whether it is signed or not.
  Nothing enrolls the test signing key on a plain GRUB boot, so a
  signed image would fail dm-verity setup with ENOKEY. dm-verity
  setup is a hard failure in systemd-sysext's `merge_subprocess`
  (`src/sysext/sysext.c`), so one such image aborts the merge for
  every other extension on the machine.

Why no `hello-broke.sysext.raw` here: the UKI directory ships that
malformed asset to assert that systemd-stub filters it out of
`.efi.extra.d/`. On the GRUB path there is no such filter -- the agent
sweep stages every `*.sysext.raw` it finds on the media and immucore
links them all into `/run/extensions`. When `systemd-sysext refresh`
then dissects each one, an image that fails the policy check (which
`hello-broke` does, since it has neither verity nor a signature) is a
fatal error for the whole merge, not a soft skip: `sd-merge` prints
`Image does not match image policy` and the service exits non-zero,
leaving even the valid `work.sysext.raw` unmerged. The corresponding
UKI assertion (that the stub filters `hello-broke` out) is exercised
in `tests/uki_test.go`; the GRUB spec does not need it.

The shipped extension carries a
`usr/lib/extension-release.d/extension-release.work` with `ID=_any`,
so systemd-sysext identifies it regardless of the host os-release.

The UKI equivalent lives in `tests/assets/sysext-uki/`. Its
`work.sysext.raw` is verity + signed with the test keys.

## Why `hello.sh` sits under `/usr/local/include`, not `/usr/local/bin`

The kairos-init drop-in `99_sysext.yaml` sets
`SYSTEMD_SYSEXT_HIERARCHIES` to the twelve `/usr/*` and `/usr/local/*`
directories a sysext is allowed to merge into. `systemd-sysext refresh`
creates an overlay at every hierarchy an extension has content for,
and on this systemd version (261) those overlays are **read-only** --
the drop-in does not set `MutableOverlays=yes` and would need to for
the merged directory to accept writes.

`/usr/local/bin` is one of the configured hierarchies, and it is also
where the install path writes: bundles declared with
`rootfs_path: /usr/local/bin` extract there, kairos-agent's
`fix-home-dir-ownership` script lives there, and so on. If the
extension's payload sits at `/usr/local/bin/hello.sh`,
`systemd-sysext` overlays `/usr/local/bin` after boot and every
subsequent write to it fails with EROFS -- the FIPS `install` spec's
edgevpn bundle stops installing, the `mkdir /usr/lib/extensions` step
in `99_sysext.yaml` logs a matching error, and the assertion for
`/usr/local/bin/usr/bin/edgevpn` fails.

Putting `hello.sh` under `/usr/local/include/kairos-test/hello.sh`
sidesteps that: `/usr/local/include` is in the drop-in's hierarchy
list, so the merge still happens and the sysext test's assertion has
something to observe, but no other install path writes there so the
resulting read-only overlay lands on an unused directory. The overlay
on `/usr/lib` from `extension-release.d/extension-release.work` is
still there and logs a non-fatal `mkdir /usr/lib/extensions:
read-only file system` warning during boot -- accept it, or open the
broader discussion about turning on `MutableOverlays=yes` in the
drop-in so `/usr/local/bin` can be a merge target the boot can also
write to.

What the extension is:

- A `/usr/local/include/kairos-test/` layer with a `hello.sh` script
  that prints the literal string `Hello world`.
  `tests/sysext_live_media_test.go` runs it via its absolute path and
  asserts on that string with `ContainSubstring("Hello world")`; keep
  the exact casing if you regenerate.
- `work.sysext.raw` is a systemd-repart DDI with only the erofs data
  and verity hash partitions (no root-verity-sig partition).

Rebuilding `work.sysext.raw` needs a custom repart definitions
directory, because `systemd-repart -S` (i.e. `--make-ddi=sysext`)
drops in the stock sysext definitions which include the verity
signature partition, and that partition insists on a signing key.
Prepare a SOURCE_DIR with `usr/local/include/kairos-test/hello.sh`
and `usr/lib/extension-release.d/extension-release.work` (with
`ID=_any`), then:

```
mkdir -p defs.d
cat > defs.d/10-root.conf <<'EOF'
[Partition]
Type=root
Format=erofs
CopyFiles=/usr/
CopyFiles=/opt/
Verity=data
VerityMatchKey=root
Minimize=best
EOF
cat > defs.d/20-root-verity.conf <<'EOF'
[Partition]
Type=root-verity
Verity=hash
VerityMatchKey=root
Minimize=best
EOF
systemd-repart --seed=00000000-0000-0000-0000-000000000000 \
    --empty=create --size=auto \
    --definitions=defs.d --root=SOURCE_DIR OUTPUT_FILE
```

The fixed seed keeps the generated UUIDs and the resulting bytes
reproducible across rebuilds. `mkfs.erofs` (Arch: `erofs-utils`;
Fedora: `erofs-utils`; Debian: `erofs-utils`) has to be on PATH.
