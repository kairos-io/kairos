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

- `work.sysext.raw` is a `/usr/bin/` layer with a `hello.sh` script that
  prints the literal string `Hello world`. `tests/uki_test.go` runs the
  script by name and asserts on that string, so keep the exact casing if
  you regenerate. The payload used to sit in `/usr/local/bin/`, which is
  where COS_PERSISTENT is mounted;
  `kairos-init/pkg/bundled/cloudconfigs/99_sysext.yaml` no longer lists
  any `/usr/local/*` path in `SYSTEMD_SYSEXT_HIERARCHIES`, because a
  successful merge makes every hierarchy it covers read-only and that
  cost the persistent partition its writable half. `/usr/bin` is where a
  sysext-delivered binary belongs anyway.
- `work.sysext.raw` is a systemd-repart DDI (erofs data + verity hash +
  verity signature partition).
- `hello-broke.sysext.raw` is a plain squashfs bake with the same script,
  still at `/usr/local/bin/hello.sh`. It is never merged (the image
  policy rejects it, which is the point of shipping it), so where its
  payload sits does not matter.

Rebuilding `work.sysext.raw` takes the same explicit definitions
directory as the GRUB asset plus a signature partition, because
`systemd-repart -S` needs systemd's stock `sysext.repart.d` installed on
the build host and fails with `DDI type 'sysext' is not defined` without
it. Prepare a SOURCE_DIR with `usr/bin/hello.sh` (mode 0755) and
`usr/lib/extension-release.d/extension-release.work` (with `ID=_any`),
write the `defs.d` from `../sysext-grub/README.md`, add:

```
cat > defs.d/30-root-verity-sig.conf <<'EOF'
[Partition]
Type=root-verity-sig
Verity=signature
VerityMatchKey=root
EOF
```

then, with a 0600 copy of the key (repart refuses a more permissive one):

```
systemd-repart --seed=00000000-0000-0000-0000-000000000000 \
    --empty=create --size=auto --offline=yes \
    --definitions=defs.d --root=SOURCE_DIR OUTPUT_FILE \
    --private-key=db.key --certificate=tests/assets/keys/db.pem
```

Rebuilding `hello-broke.sysext.raw` (with
[sysext-bakery](https://github.com/flatcar/sysext-bakery), which does not
support signing or verity):

```
bake.sh SOURCE_DIR
```
