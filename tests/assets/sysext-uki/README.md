Sysext test extensions for the Trusted Boot (UKI) live-media sweep.

This directory is baked onto every UKI test ISO by `_build-iso.yaml` via
auroraboot's `--overlay-iso`. Two extensions ship:

- `work.sysext.raw`: verity + signed. The verity root-hash is signed
  with the test signing key `tests/assets/keys/db.key`, and the
  matching certificate is `tests/assets/keys/db.pem`. A UKI boot
  enrolls `tests/assets/keys/db.auth` (the EFI signed variable derived
  from `db.pem`) into the Secure Boot db, so the kernel trusts the
  signature and systemd-sysext merges the extension.
- `hello-broke.sysext.raw`: neither verity nor signed. It is rejected by
  the image policy the UKI drop-in installs, and it is there to assert
  that a broken extension does not take the valid one down with it.

Both extensions carry a `usr/lib/extension-release.d/extension-release.NAME`
with `ID=_any`, so systemd-sysext identifies them regardless of the host
os-release.

The plain GRUB equivalent lives in `tests/assets/sysext-grub/`. Its
`work.sysext.raw` is verity-only (unsigned), because a GRUB boot does not
enroll the test keys and a signed image would fail dm-verity setup with
ENOKEY, which is an all-or-nothing failure for the whole sysext merge.
The split keeps that assertion out of the GRUB path.

What the extensions are:

- Each is a `/usr/local/bin/` layer with a `hello.sh` script that prints
  the literal string `Hello world`. **That payload no longer reaches the
  host.** `/usr/local` is the COS_PERSISTENT mount and
  `kairos-init/pkg/bundled/cloudconfigs/99_sysext.yaml` no longer lists
  any `/usr/local/*` path in `SYSTEMD_SYSEXT_HIERARCHIES`, because a
  successful merge makes every hierarchy it covers read-only. The
  extension still merges, through the
  `usr/lib/extension-release.d/extension-release.work` it carries, and
  `tests/uki_test.go` and `tests/sysext_live_media_test.go` assert on
  that file instead. Regenerate this image with the script at
  `/usr/bin/hello.sh` and both specs can go back to running the command;
  keep the exact casing of `Hello world` if you do.
- `work.sysext.raw` is a systemd-repart DDI (erofs data + verity hash +
  verity signature partition).
- `hello-broke.sysext.raw` is a plain squashfs bake with the same script.

Rebuilding `work.sysext.raw`:

```
systemd-repart -S -s SOURCE_DIR OUTPUT_FILE \
    --private-key=tests/assets/keys/db.key \
    --certificate=tests/assets/keys/db.pem
```

Rebuilding `hello-broke.sysext.raw` (with
[sysext-bakery](https://github.com/flatcar/sysext-bakery), which does not
support signing or verity):

```
bake.sh SOURCE_DIR
```
