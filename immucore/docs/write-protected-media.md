# Write-protected media

How immucore boots a unit whose disk is write-protected in hardware. The
flags are listed in the [README](../README.md#kernel-configuration-parameters);
this page is the layout, the reasoning and the limits.

Some units ship with a drive that is write-protected in hardware. The unit is
installed normally while the drive is still writable, the switch is flipped, and
from then on every boot sees a block device the kernel refuses writes to.

Told to expect it (`rd.immucore.write_protected` on the cmdline), immucore
detects that and changes the layout so the machine still boots and its
applications can still write. Reads fall through to whatever provisioning left on
the persistent partition; writes go to RAM and are gone on the next boot.

## What the layout looks like

| What | Where it ends up |
|---|---|
| `COS_PERSISTENT` | the origin of a device-mapper snapshot, `/dev/mapper/kairos-persistent` |
| the snapshot's copy-on-write store | a sparse file on its own tmpfs at `/run/immucore/cow`, attached as a loop device |
| `/usr/local` | the snapshot, mounted read-write like the partition would be |
| `/usr/local/.state/*.bind` binds | unchanged, and writable |
| `COS_OEM` | mounted read-only |
| `COS_STATE` and the root image | mounted `ro` plus the filesystem's no-recovery option |

The snapshot is a block device. A read of a chunk nobody has written since boot
comes from the partition; a write copies the 4 KiB chunk into the store and goes
there, and so does every later read of it. The filesystem on top is the
partition's own ext4, mounted read-write, and it has no idea the disk underneath
refuses writes. Its journal replays into the store like any other write, so a
unit that lost power before it was write-protected still boots cleanly.

It is a block device rather than a file-level overlay for what runs on top. A
container runtime's snapshotter is itself overlayfs, and the kernel refuses an
overlayfs whose upper layer sits on another overlayfs, so k3s and k0s could not
start a single pod on the earlier design. On the snapshot they run as they do on
any disk, and container images pre-seeded on the partition are used in place
rather than copied. This is the same mechanism dracut's `dmsquash-live` uses for
the root of every live boot.

Nothing under `/usr/local` survives a reboot. Everything that was there when the
drive was write-protected is still readable.

## Why a snapshot and not an overlay

The first version of this mounted the frozen partition read-only and put an
overlayfs on top of it with a tmpfs upper layer, so `/usr/local` looked
writable and the binds worked. It booted, and then k0s could not start a
single pod:

```
failed to mount overlay: ... upperdir=/var/lib/k0s/containerd/io.containerd.snapshotter.v1.overlayfs/...: invalid argument
```

The kernel refuses an overlayfs whose upper layer is itself on overlayfs
(`ovl_mount_dir_check()` rejects an upperdir with `DCACHE_OP_REAL`, which is
what an overlayfs mount sets). containerd's overlayfs snapshotter, which k3s
and k0s use, keeps its upper and work directories under
`/var/lib/{rancher,k0s}/containerd`, a bind from `/usr/local/.state`, which
was on the overlay. So the layer containerd needs cannot be created, on any
kernel, by design.

The ways around that are all worse. containerd's `native` snapshotter works
but copies every image layer into a directory per container, so each pod
costs RAM equal to its image and the images pre-seeded on the disk cannot be
used in place. overlayfs itself copies whole files up on first write, so
appending one byte to a 2 GB pre-seeded file costs 2 GB of RAM.

A block-level snapshot has none of these problems. `dm-snapshot` sits under
the filesystem: the frozen partition is the origin, a sparse file on a tmpfs
is the copy-on-write store, and what gets mounted at `/usr/local` is the
partition's own ext4, read-write, with nothing above it that containerd can
see. Unchanged 4 KiB chunks are read from the disk, changed chunks are copied
to RAM once, pre-seeded images are used where they are, and the ext4 journal
replays into the store like any other write, so a unit that lost power before
it was frozen still boots cleanly. It is the same mechanism dracut's
`dmsquash-live` uses for the root of every live Kairos boot, and the module is
already in the initramfs.

What it costs: the store counts sectors, not files, so it cannot say which
files filled it, and sizing has to be done by running the real workload once
and watching `persistent_cow.used_sectors`.

## How it decides

The kernel is asked directly, with the `BLKROGET` ioctl, whether it refuses
writes to the device. The persistent partition is asked first, then its LUKS
container, then the state and recovery partitions, and the first that answers
decides. Those are udev symlinks, so immucore waits up to ten seconds for one to
appear before concluding it cannot tell, in which case it assumes writable media
and says so in the log. A dm-crypt mapper is also checked against the devices
underneath it, as insurance.

The feature is opt-in. Without `rd.immucore.write_protected` on the cmdline
immucore does not even ask the device, and a frozen disk fails the way it
always did. With it:

| cmdline | Behaviour |
|---|---|
| absent, `=0` | off: no probe, no sentinel, the ordinary layout |
| `rd.immucore.write_protected`, `=1`, `=auto` | enabled: the device decides; the layout applies only if it is write-protected |
| `=force` | the layout applies without asking, for testing on a writable disk |

The last one on the cmdline wins. Every other write filter (Windows UWF, Deep
Freeze, `overlayroot`, `systemd.volatile=`) is off until turned on, and so is
this.

Bake the flag into the install, because `/oem/grubenv` cannot be edited once
the disk is frozen:

```yaml
install:
  grub_options:
    extra_cmdline: "rd.immucore.write_protected"
```

That is why "enabled" asks the device rather than forcing the layout: with the
flag baked in, the writable first boot stays ordinary and saves its machine-id,
hostname and cluster state to disk, and the layout switches on by itself the
first time the disk boots frozen.

## What is not written, and why

* `fsck` does not run. The default is a repairing fsck (`fsck.mode=auto` with
  `fsck.repair=preen`), it runs before the mount, so no mount option can prevent
  it, and it would be the first write of the boot. `fsck.mode=skip` was always
  the manual way out; read-only media now implies it.
* The journals of `COS_STATE`, `COS_OEM` and the root image are not replayed.
  `ro` on its own is not enough for ext4: a dirty journal is replayed even on a
  read-only mount unless `noload` is given. xfs spells the same thing
  `norecovery`. The persistent partition needs none of this, because its writes
  land in the snapshot's store.
* The persistent partition is not grown, the GRUB environment is not rewritten,
  and the LUKS headers are not upgraded. Each of those wrote to the disk on every
  boot; they are now skipped.

## Reporting

immucore writes `/run/cos/write_protected`, additively, alongside the usual
boot-state sentinel, so the existing cloud-config gates keep firing. Gate a
cloud-config stage on it the way the shipped ones do:

```yaml
- if: '[ ! -f /run/cos/write_protected ]'
  name: "something that writes to the disk"
```

`kairos-agent state` reports it as `write_protected`, and how full the store is
under `persistent_cow`, in 512-byte sectors:

```
$ kairos-agent state get write_protected
true
$ kairos-agent state get persistent_cow.state
active
$ kairos-agent state get persistent_cow.used_sectors
57344
$ kairos-agent state get persistent_cow.total_sectors
2847168
```

`dmsetup status kairos-persistent` is the same information straight from the
kernel, as `<used>/<total> <metadata>`.

## Sizing the store, which is the thing most likely to bite

Every write to the persistent tree consumes RAM out of the store, and the store
only grows: a chunk copied in stays there until power-off, even if the file it
belonged to is deleted, and writing the same chunk twice costs it once. When the
store is full, the persistent filesystem starts refusing writes. Reads keep
working, so a node in that state is still reachable and `persistent_cow` says
`overflow`; it does not recover without a reboot.

The store lives on its own tmpfs, sized by `WRITE_PROTECTED_COW` in
`cos-layout.env` or `rd.immucore.write_protected.cow=` on the cmdline. The
value is a size in tmpfs syntax (`2G`, `512M`, `25%`); the `OVERLAY` spelling
`tmpfs:<size>` is accepted too, and so is `LABEL=<label>` or `UUID=<uuid>` for
a filesystem on a second, writable disk to hold the store instead of RAM,
though that form is untested. Neither set, it is sized like `OVERLAY`,
which the shipped `00_rootfs.yaml` makes `tmpfs:25%` of RAM. The store claims
95% of that tmpfs, so that it reports full before the tmpfs underneath it runs
out of space, and the tmpfs holds nothing else. The size is a ceiling, not a
reservation: the file is sparse and the tmpfs only takes RAM as chunks are
written. Set it from a cloud-config stage on a stock image, as `OVERLAY` from
`cos-layout.env` overrides the cmdline:

```yaml
stages:
  rootfs:
    - name: "Size the copy-on-write store"
      environment_file: /run/cos/cos-layout.env
      environment:
        WRITE_PROTECTED_COW: "2G"
```

Container images pulled after boot, the per-boot rsync into each
`.state/*.bind` directory, and logs all land in the store. Pre-seed bulk content
onto the partition while it is still writable and size the store for the
deltas. The store counts sectors, not files, so it cannot say which files
filled it; size it the way Windows recommends for its write filter, by running
the real workload on a test unit and watching `persistent_cow.used_sectors`
over the longest interval between reboots.

## Limits

* Only normal and in-RAM boots get the layout. On UKI, udevd is started by
  immucore's own UKI step, so nothing can be detected before the graph runs; on
  live media the layout would skip the cdrom datasource stage. Both keep the
  ordinary layout, and a frozen disk behind them fails the way it always did.
* The kernel needs `dm-snapshot`. The Kairos initramfs ships it, and
  `kairos-init validate` warns when the kernel in an image does not have it.
* LVM installs are not detected. The persistent, state and recovery labels are on
  logical volumes that do not exist until LVM activation, which runs inside the
  graph, after detection. Pass `rd.immucore.write_protected=force` on such a
  unit, once the disk is frozen.
* Encrypted persistent partitions do not unlock on write-protected media. kcrypt
  opens the mapper through `anatol/luks.go`, which never sets the device-mapper
  read-only flag, and the kernel refuses a read-write mapper over a device it
  cannot write. The fix belongs upstream; until it lands, do not write-protect an
  encrypted unit.
* Boot once before write-protecting. The first boot saves the machine-id and
  hostname into `/usr/local`; a unit frozen before that gets a fresh machine-id
  on every boot.
* Upgrades and resets are impossible. They write to `COS_STATE`.
* Userdata has to be in place before the drive is write-protected. `/oem` is
  read-only, so the datasource stage that would write `/oem/95_userdata` is
  skipped: a config that only arrives at boot from a cdrom or NoCloud source
  cannot be persisted.
* Writes an operator makes to `/oem` fail, on purpose. `/oem` holds authored
  configuration, and `/oem/grubenv` is read by GRUB before Linux exists, so a
  write landing in RAM could never affect the next boot. An error is more use
  than a write that appears to succeed and disappears.
* Nothing on the frozen disk can be made to persist. Software write filters
  offer exclusions, a thaw space or a commit command for the few files that
  should survive; here the filter is the hardware, and there is no path
  through it. What has to persist across reboots goes on a second device that
  is not write-protected.
* If no `VOLUMES` entry names the persistent mountpoint there is nothing to
  snapshot, and the layout is whatever the same configuration gives on a
  writable disk.
* If the snapshot cannot be made, the boot fails loudly. immucore's failure
  summary is painted on the console, and with the `rd.emergency=reboot` the
  Kairos cmdline carries, dracut reboots. Persistent state cannot be mounted
  without it, so booting on without it would mean a node that looks up with
  none of its state.
