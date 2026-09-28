Sysext test extensions for the plain GRUB live-media sweep.

This directory is baked onto every non-UKI test ISO by `_build-iso.yaml`
via auroraboot's `--overlay-iso`. Two extensions ship:

- `work.sysext.raw`: verity, **unsigned**. The GRUB drop-in installed by
  `kairos-init` runs `systemd-sysext refresh` with
  `--image-policy="root=verity+absent:usr=verity+absent"`, which accepts
  a verity-protected image whether it is signed or not. Nothing enrolls
  the test signing key on a plain GRUB boot, so a signed image would
  fail dm-verity setup with ENOKEY. dm-verity setup is a hard failure
  in systemd-sysext's merge_subprocess (`src/sysext/sysext.c`), so one
  such image aborts the merge for every other extension on the machine.
  Keeping the GRUB asset unsigned lets the merge succeed on both this
  extension and anything else the boot installs (bundles, declared
  extensions, ...).
- `hello-broke.sysext.raw`: neither verity nor signed. The image policy
  above rejects it, but as an incompatible image, which is the forgiving
  branch of the merge (`n_ignored++; continue`), so it does not take
  `work.sysext.raw` down with it. It is here to assert that behavior on
  the GRUB path.

Both extensions carry a `usr/lib/extension-release.d/extension-release.NAME`
with `ID=_any`, so systemd-sysext identifies them regardless of the host
os-release.

The UKI equivalent lives in `tests/assets/sysext-uki/`. Its
`work.sysext.raw` is verity + signed with the test keys.

What the extensions are:

- Each is a `/usr/local/bin/` layer with a `hello.sh` script that prints
  the literal string `Hello world`. `tests/sysext_live_media_test.go`
  asserts on that string with `ContainSubstring("Hello world")`; keep
  the exact casing if you regenerate.
- `work.sysext.raw` is a systemd-repart DDI with only the erofs data and
  verity hash partitions (no root-verity-sig partition).
- `hello-broke.sysext.raw` is a plain squashfs bake with the same
  script; identical to the file in `tests/assets/sysext-uki/`.

Rebuilding `work.sysext.raw` needs a custom repart definitions directory,
because `systemd-repart -S` (i.e. `--make-ddi=sysext`) drops in the stock
sysext definitions which include the verity signature partition, and
that partition insists on a signing key. Prepare a SOURCE_DIR with
`usr/local/bin/hello.sh` and
`usr/lib/extension-release.d/extension-release.work` (with `ID=_any`),
then:

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
reproducible across rebuilds. `mkfs.erofs` (Arch: `erofs-utils`; Fedora:
`erofs-utils`; Debian: `erofs-utils`) has to be on PATH.

Rebuilding `hello-broke.sysext.raw` follows the same procedure as in
`tests/assets/sysext-uki/README.md`. If you regenerate it, keep the two
files byte-identical (they are the same asset).
