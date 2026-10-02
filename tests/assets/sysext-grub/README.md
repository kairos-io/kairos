Sysext test extensions for the plain GRUB live-media sweep.

This directory is baked onto every non-UKI test ISO by `_build-iso.yaml`
via auroraboot's `--overlay-iso`. One extension ships:

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

Why no `hello-broke.sysext.raw` here: the UKI directory ships that
malformed asset to assert that systemd-stub filters it out of
`.efi.extra.d/`. On the GRUB path there is no such filter in the
firmware. Since kairos-io/kairos#4989 immucore validates each image
against the policy that boot enforces and does not link one that fails
it, so `hello-broke` would be dropped before `systemd-sysext refresh`
ever saw it. That is worth asserting, but it belongs in a spec about
immucore's filter rather than in this one: without the filter, an image
that fails the policy check is a fatal error for the whole merge, not a
soft skip, and `work.sysext.raw` would stay unmerged for a reason that
has nothing to do with the live media sweep this directory exists to
cover. The corresponding UKI assertion (that the stub filters
`hello-broke` out) is exercised in `tests/uki_test.go`; the GRUB spec
does not need it.

The one shipped extension carries a
`usr/lib/extension-release.d/extension-release.work` with `ID=_any`, so
systemd-sysext identifies it regardless of the host os-release.

The UKI equivalent lives in `tests/assets/sysext-uki/`. Its
`work.sysext.raw` is verity + signed with the test keys.

What the extension is:

- A `/usr/local/bin/` layer with a `hello.sh` script that prints the
  literal string `Hello world`. **That payload no longer reaches the
  host.** `/usr/local` is the COS_PERSISTENT mount and
  `kairos-init/pkg/bundled/cloudconfigs/99_sysext.yaml` no longer lists
  any `/usr/local/*` path in `SYSTEMD_SYSEXT_HIERARCHIES`, because a
  successful merge makes every hierarchy it covers read-only. The
  extension still merges, through the
  `usr/lib/extension-release.d/extension-release.work` it carries, and
  `tests/sysext_live_media_test.go` asserts on that file instead.
  Regenerate this image with the script at `/usr/bin/hello.sh` and the
  spec can go back to running the command; keep the exact casing of
  `Hello world` if you do.
- `work.sysext.raw` is a systemd-repart DDI with only the erofs data and
  verity hash partitions (no root-verity-sig partition).

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
