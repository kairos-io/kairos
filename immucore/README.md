<h1 align="center">
  <br>
     <img width="184" alt="kairos-white-column 5bc2fe34" src="https://user-images.githubusercontent.com/2420543/193010398-72d4ba6e-7efe-4c2e-b7ba-d3a826a55b7d.png"><br>
    Immucore
<br>
</h1>

<h3 align="center">The Kairos immutability management interface </h3>
<p align="center">
  <a href="https://opensource.org/licenses/">
    <img src="https://img.shields.io/badge/licence-APL2-brightgreen"
         alt="license">
  </a>
  <a href="https://github.com/kairos-io/kairos/issues"><img src="https://img.shields.io/github/issues/kairos-io/kairos"></a>
  <a href="https://kairos.io/docs/" target=_blank> <img src="https://img.shields.io/badge/Documentation-blue"
         alt="docs"></a>
  <img src="https://img.shields.io/badge/made%20with-Go-blue">
</p>


> Immucore lives in the Kairos monorepo. See the
> [root README](../README.md) for the full repository layout. Import
> path: `github.com/kairos-io/kairos/v4/immucore`.

## What is Immucore?

---

Immucore is the management interface to mount Kairos disks and filesystems.
It is a dracut module responsible for mounting the root tree during boot time with the specific immutable setup.
The immutability concept refers to read only root (/) system.
To ensure the linux OS is still functional certain filesystem paths are required to be writable,
in those cases an ephemeral overlay tmpfs filesystem is set in place. Ephemeral refers that changes to files or dirs in this filesystem will be lost upon reboot.

Additionally, the immutable rootfs module can also mount a custom list of device blocks with read write permissions, those are mostly devoted to store persistent data.


Immucore is mostly configured via kernel command line parameters or via the `/run/cos/cos-layout.env` environment file.

These are the read write paths the module mounts as part of the overlay
ephemeral tmpfs: `/etc`, `/root`, `/home`, `/opt`, `/srv`, `/usr/local`
and `/var`.


## Kernel configuration parameters

The immutable rootfs can be configured with the following kernel parameters:

* `cos-img/filename=<imgfile>`: This is one of the main parameters, it defines
  the location of the image file to boot from. This defines the booting mode for
  Immucore, setting in motion the full workflow to end up with an immutable system.

* `rd.immucore.overlay=tmpfs:<size>`: This defines the size of the tmpfs used for
  the ephemeral overlayfs. It can be expressed in MiB or as a % of the available
  memory. Defaults to `rd.immucore.overlay=tmpfs:20%` if not present.
  Backwards compatible with the old `rd.cos.overlay` directive.

* `rd.immucore.overlay=LABEL=<vol_label>`: Optionally and mostly for debugging
  purposes the overlayfs can be mounted on top of a persistent block device.
  Block devices can be expressed by LABEL (`LABEL=<blk_label>`) or by UUID
  (`UUID=<blk_uuid>`)
  Backwards compatible with the old `rd.cos.overlay` directive.

* `rd.immucore.mount=LABEL:<blk_label>:<mountpoint>`: This option defines a
  persistent block device and its mountpoint. Block devices can also be
  defined by UUID (`UUID=<blk_uuid>:<mountpoint>`). This option can be passed
  multiple times.
  Backwards compatible with the old `rd.cos.mount` directive.

* `rd.immucore.oemlabel=<label>`: This option sets the label to search for in order
  to mount the OEM partition. Defaults to COS_OEM
  Backwards compatible with the old `rd.cos.oemlabel` directive.

* `rd.immucore.oemtimeout=<seconds>`: By default we assume the existence of a
  persistent block device labelled `COS_OEM` which is used to keep some
  configuration data (mostly cloud-init files). The immutable rootfs tries
  to mount this device at very early stages of the boot even before applying
  the immutable rootfs configs. It's done this way to enable the configuration of the
  immutable rootfs module within the cloud-init files. As the `COS_OEM` device
  might not be always present, the boot process just continues without failing
  after a certain timeout. This option configures such a timeout. Defaults to
  5s.
  Backwards compatible with the old `rd.cos.oemtimeout` directive.

* `rd.cos.debugrw`/`rd.immucore.debugrw`: This is a boolean option, true if present, false if not.
  This option sets the root image to be mounted as a writable device. Note that this
  completely breaks the concept of an immutable root. This is helpful for
  debugging or testing purposes, so changes persist across reboots.

* `rd.cos.disable`/`rd.immucore.disable`: This is a boolean option, true if present, false if not.
  It disables the execution of any immutable rootfs module logic at boot.

* `rd.immucore.debug`: Enables debug logging

* `rd.immucore.uki`: Enables UKI booting

* `rd.immucore.break=<step>`: Stops the boot right before `<step>` and hands the
  console to an interactive shell, like dracut's `rd.break`. The boot resumes
  when that shell exits, so this is for looking around mid-boot, not for
  recovering from a failure (that shell still comes up on its own). Valid values
  are the DAG step names (not the `<init>` node herd itself adds), e.g.
  `rd.immucore.break=mount-root` or `rd.immucore.break=uki-pivot-to-sysroot`.
  Several steps can be given comma-separated (`rd.immucore.break=load-config,mount-root`)
  or by repeating the stanza. A name that matches no step is ignored. To see all
  available step names for your boot configuration, run `immucore --dry-run` to
  display the full DAG.

  There is a ceiling on how long you can sit at a breakpoint: systemd bounds the
  start of `immucore.service` by its start timeout (`TimeoutStartSec=` on the
  unit, or `DefaultTimeoutStartSec` from `systemd-system.conf` when the unit
  sets none). When that expires systemd moves on to
  `initrd-switch-root.target` with none of immucore's mounts done and nothing
  said on the console — the same silent boot-continue the `RebootOrWait` doc
  comment in `internal/utils/common.go` describes. Raise `TimeoutStartSec=` on
  `immucore.service` if you need to hold a breakpoint open longer than that.

* `rd.immucore.write_protected`: Enables the write-protected media layout.
  Absent, immucore never asks the block device and a frozen disk fails as it
  always did. Present on its own, or as `=1` or `=auto`, immucore asks the
  device and applies the layout only if it is write-protected, so the flag can
  be baked into an install while the disk is still writable. `=0` turns it
  off; `=force` applies the layout without asking, for testing on a writable
  disk. See
  [Read-only media boot](#read-only-media-boot-rdimmucorewrite_protected) below for
  what the layout actually does.

* `rd.immucore.write_protected.cow=<size>`: On read-only media, sizes the
  tmpfs that holds the persistent partition's copy-on-write store. `2G`,
  `25%` and `tmpfs:2G` all mean a tmpfs of that size.
  `WRITE_PROTECTED_COW` in `cos-layout.env` is the same knob and wins over the
  cmdline. Absent, the store is sized like `rd.immucore.overlay=`. Does nothing
  on a writable disk. See Read-only media boot below.

* `rd.immucore.sysrootwait=<seconds>`: Waits for the sysroot to be mounted up to <seconds> before continuing with the boot process. This is useful when booting from CD/Netboot as immucore doesn't mount the /sysroot in those cases, but we want to run the initramfs stage once the system is ready. Sometimes dracut can be really slow and the default 1 minute of waiting is not enough. In those cases you can increase this value to wait more time. Defaults to 60s.

### Read-only media boot (`rd.immucore.write_protected`)

---

Some units ship with a drive that is write-protected in hardware. The unit is
installed normally while the drive is still writable, the switch is flipped, and
from then on every boot sees a block device the kernel refuses writes to.

Told to expect it (`rd.immucore.write_protected` on the cmdline), immucore
detects that and changes the layout so the machine still boots and its
applications can still write. Reads fall through to whatever provisioning left on
the persistent partition; writes go to RAM and are gone on the next boot.

#### What the layout looks like

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

#### How it decides

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

#### What is not written, and why

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

#### Reporting

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

#### Sizing the store, which is the thing most likely to bite

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

#### Limits

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

### In-RAM boot (`kairos.ram.*`)

---

The in-RAM workflow boots the OS entirely from memory (livecd/PXE style) while
still mounting the local `COS_OEM` and `COS_PERSISTENT` partitions from disk,
so cloud-config and user data behave exactly like on an installed system. The
typical user is a PXE-served fleet: every machine boots the same image over
the network, per-machine state lives on the local disk, and "upgrading" means
swapping the image on the PXE server and rebooting. No `COS_STATE` /
`COS_ACTIVE` partitions are needed on the disk.

Setting any `kairos.ram.*` stanza enables the mode; the bare `kairos.ram`
token is only needed when no other stanza is present.

* `kairos.ram`: Enables the in-RAM workflow.

* `kairos.ram.create_partitions`: On first boot, if `COS_OEM` and/or
  `COS_PERSISTENT` are missing, create (and format) them automatically. With
  no value, the largest EMPTY candidate (non-removable, non-virtual) disk is
  auto-selected — disks that already carry a partition table are likely in
  use by another system, so they are only picked when no empty disk exists
  (and then the wipe guard below still applies). Largest-first matches the
  rule kairos-agent uses for `device: auto` at install time. Boot stops with
  a message only when no eligible disk exists at all. Existing partitions
  are never touched: if one of the two labels already exists, only the
  missing one is created.

* `kairos.ram.create_partitions=<device>`: Same, but target an explicit disk
  (e.g. `kairos.ram.create_partitions=/dev/vda`). The consent rules below
  still apply — an explicit disk that belongs to another system is refused
  without `kairos.ram.wipe`, so a typo in the device path cannot destroy or
  alter a foreign disk.

* `kairos.ram.wipe`: Consent flag for touching disks that already carry
  partitions belonging to another system. **Destroys all data on the target
  disk.** Without this flag such a disk stops the boot with an explanation.
  With it, auto-selection also skips the empty-disk preference and simply
  takes the largest disk, whatever its state.

* `kairos.ram.oem=<MiB>`: Size of the created `COS_OEM` partition in MiB.
  Defaults to 64.

* `kairos.ram.persistent=<MiB>`: Size of the created `COS_PERSISTENT`
  partition in MiB. Defaults to 0, which means "expand to the end of the
  disk".

#### Disk selection and consent rules

How the target disk is resolved when partitions need creating:

| Selection | `kairos.ram.wipe` | Target |
|---|---|---|
| `create_partitions=/dev/X` | any | `/dev/X`, verbatim |
| bare `create_partitions` | unset | largest EMPTY candidate disk; if none is empty, largest overall (then hits the consent rule below) |
| bare `create_partitions` | set | largest candidate disk, regardless of state |

And what happens to the resolved target:

| Target disk state | `kairos.ram.wipe` | Result |
|---|---|---|
| Empty (no partition table) | any | fresh GPT + partitions created |
| Already carries `COS_OEM` or `COS_PERSISTENT` | any | append-only: the missing label is created next to the existing one, nothing else is touched |
| Carries only foreign partitions | unset | **boot halts** with the wipe-required screen |
| Carries only foreign partitions | set | fresh GPT when both labels are missing (destroys the disk), append otherwise |

Candidate disks exclude removable media (USB, SD), CD-ROM and virtual
devices (loop, ram, zram, nbd, device-mapper, md). Largest-first matches the
rule kairos-agent uses for `device: auto` at install time.

When something blocks the boot (missing partitions and no
`create_partitions` flag, no eligible disk, foreign disk without `wipe`),
immucore takes over the console with a full-screen message explaining what
went wrong and the exact stanzas to fix it. On systemd systems, pressing any
key reboots immediately, and with no input the system reboots automatically
after 90 seconds through `systemd-reboot.service` (so boot-assessment sees
the failed boot). On non-systemd systems (e.g. Alpine) the message is
printed and the boot fails normally.

Sentinel: in-RAM boots are classified as `active_boot` (the running system
is the current install), so `/run/cos/active_mode` is written as usual, plus
an additional `/run/cos/in_ram_mode` sentinel for tooling that needs to know
the rootfs lives in a tmpfs.

#### Trusted boot (UKI)

The same `kairos.ram.*` stanzas work under trusted boot. A UKI already runs
entirely from RAM, so the regular UKI flow applies with three differences:

* `create_partitions` encrypts the partitions it creates with the TPM PCR
  policy (same `systemd-cryptenroll` enrollment kairos-agent performs on a
  trusted-boot install), so every later boot unlocks them via TPM. There is
  no plaintext fallback: if encryption fails the boot halts with a
  full-screen message. Remote KMS (kcrypt-challenger) is not supported here —
  the UKI initramfs has no network at unlock time.
* Partition unlock runs even though the UKI was booted from removable or
  network media (a regular UKI boot skips it in that case).
* The UKI sentinel is `/run/cos/uki_boot_mode`, not `uki_install_mode`, so
  installer cloud-init stages do not fire.

Since the cmdline is part of the signed UKI (or a signed addon), the
`kairos.ram.*` stanzas must be baked in at image build time — they cannot be
edited interactively at boot.

Because the sentinel is `uki_boot_mode`, the stock datasource cloud-config
skips pulling providers (NoCloud/cidata etc.) — on an installed UKI system
the config was baked into OEM at install time, but an in-RAM boot has no
install. To feed per-machine config through a datasource instead of the OEM
partition, also bake `kairos.pull_datasources` into the cmdline.


### Configuration with an environment file

---

The immutable rootfs can be configured with the `/run/cos/cos-layout.env`
environment file. It is important to note that all the immutable root
configuration is applied in initrd before switching root and after
`rootfs` cloud-init stage but before `initramfs` stage. So the immutable rootfs
configuration via cloud-init using the `/run/cos/cos-layout.env` file is
only effective if called in any of the `rootfs.before`, `rootfs` or
`rootfs.after` cloud-init stages.


In the environment file, a few options are available:

* `VOLUMES=LABEL=<blk_label>:<mountpoint>`: This variable expects a block device
  and its mountpoint pair space separated list. The default cOS configuration is:

  `VOLUMES="LABEL=COS_OEM:/oem LABEL=COS_PERSISTENT:/usr/local"`

* `OVERLAY`: It defines the underlying device for the overlayfs as in
  `rd.cos.overlay=` kernel parameter.

* `MERGE=true`: When set, it makes the `VOLUMES` values to be merged with any other
  volume that might have been defined in the kernel command line. The merging
  criteria is simple: any overlapping volume is overwritten, all others are
  appended to whatever was already defined as a kernel parameter. If not
  defined defaults to `true`.

* `RW_PATHS`: This is a space separated list of paths. These are the paths
  that will be used for the ephemeral overlayfs. These are the paths that
  will be mounted as overlay on top of the `OVERLAY` (or `rd.cos.overlay`)
  device. Default value is:

  `RW_PATHS="/etc /root /home /opt /srv /usr/local /var"`
  **Note**: as those paths are overlay with an ephemeral mount (`tmpfs`),
  additional data wrote on those location won't be available on subsequent boots.

* `PERSISTENT_STATE_TARGET`: This is the folder where the persistent state data
  will be stored, if any. Default value is `/usr/local/.state`.

* `PERSISTENT_STATE_PATHS`: This is a space separated list of paths. These are
  the paths that will become writable and store its data inside
  `PERSISTENT_STATE_TARGET`. By default, this variable is empty, which means
  no persistent state area is created or used.

  **Note**: The specified paths needs either to exist or be located in an area
  which is writeable ( for example, inside locations specified with `RW_PATHS`).
  The dracut module will attempt to create non-existant directories,
  but might fail if the mountpoint where they are located is read-only.

* `PERSISTENT_STATE_BIND="true|false"`: When this variable is set to true
  the persistent state paths are bind mounted (instead of using overlayfs)
  after being mirrored with the original content. By default, this variable is
  set to `false`.

Note that persistent state is set up once the ephemeral paths and persistent
volumes are mounted. Persistent state paths can't be an already existing mount
point. If the persistent state requires any of the paths that are part of the
ephemeral area by default, then `RW_PATHS` needs to be defined to avoid
overlapping paths.

For example a common cOS configuration can be expressed as part of the
cloud-init configuration as follows:

```yaml
name: example
stage:
  rootfs:
    - name: "Layout configuration"
      environment_file: /run/cos/cos-layout.env
      environment:
        VOLUMES: "LABEL=COS_OEM:/oem LABEL=COS_PERSISTENT:/usr/local"
        OVERLAY: "tmpfs:25%"
```

You can also see the default config that we provide in https://github.com/kairos-io/kairos/blob/master/overlay/files/system/oem/11_persistency.yaml

## What is the default workflow of Immucore

----

It starts pretty early in the boot process, just after `systemd-udev-settle.service` and before `dracut-initqueue.service`.
The settle unit is ordered before Immucore but only pulled in with `Wants=`, so Immucore still starts when that unit is
absent or times out. See [#1378](https://github.com/kairos-io/kairos/issues/1378).
To see the full bootup process from dracut you can check [here](https://man7.org/linux/man-pages/man7/dracut.bootup.7.html).

Just after starting, Immucore mounts `/proc` if it's not mounted, it does so in order to read the `/proc/cmdline` and obtains the different stanzas in order to configure itself.
After checking the cmdline, it knows in which path is being booted, either active/passive/recovery or Netboot/LiveCD/Do nothing.

Based on that it builds a [DAG](https://en.wikipedia.org/wiki/Directed_acyclic_graph) with the steps needed to complete and process through the DAG until its completed. It also builds a `State` object which has all the configs needed to mount and configure the system properly.
Once the DAG has been completed (and with no errors), Immucore its finished, and it's ready for the initramfs init process to do a switch_root and pivot into the final root to boot the system.

When booting from Netboot/LiveCD/Do nothing (`rd.cos.disable` or `rd.immucore.disable` on the cmdline) the DAG is pretty simple. 
It proceeds to create a sentinel file under `/run/cos/` with the boot mode (`live_mode`), so cloud configs can identify that they are booting from live media and ends.


When booting from active/passive/recovery the DAG gets a bit more complicated. You can see the default DAG for an active/passive/recovery system by running Immucore with `--dry-run`.

```bash
1.
 <init> (background: false) (weak: false)
2.
 <mount-state> (background: false) (weak: false)
 <mount-base-overlay> (background: false) (weak: false)
 <mount-tmpfs> (background: false) (weak: false)
 <create-sentinel> (background: false) (weak: false)
3.
 <discover-state> (background: false) (weak: false)
4.
 <mount-root> (background: false) (weak: false)
5.
 <mount-oem> (background: false) (weak: false)
6.
 <rootfs-hook> (background: false) (weak: false)
7.
 <load-config> (background: false) (weak: false)
8.
 <custom-mount> (background: false) (weak: false)
 <overlay-mount> (background: false) (weak: false)
9.
 <mount-bind> (background: false) (weak: false)
10.
 <write-fstab> (background: false) (weak: true)
```

As shown in the DAG, the steps are in order and that shows their dependencies, i.e. `mount-root` depends on `discover-state` and that is why it's just below it.
It won't run until the previous step has completed **without errors**.
There is also the `weak` value which indicates that this step has weak dependencies. It will run even if its dependencies failed, instead of refusing to run.



### Steps explained

 - `mount-state`: Will mount the `COS_STATE` partition under `/run/initramfs/cos-state`
 - `mount-tmpfs`: Will mount `/tmp` 
 - `create-sentinel`: Will create the sentinel file identifying the boot mode (`active_mode`, `passive_mode`, `recovery_mode` or `live_mode`) under `/run/cos/`
 - `mount-base-overlay`: Will mount the base overlay under `/run/overlay`
 - `discover-state`: Will find the correct image under `/run/initramfs/cos-state` and mount it as a loop device
 - `mount-root`: Will mount the `/dev/disk/by-label/$LABEL` device under the sysroot (Usually `/sysroot`). This label is set in grub depending on the selected entry, as part of the cmdline (i.e. `root=LABEL=COS_ACTIVE`) 
 - `mount-oem`: Will **try** to mount the oem label device under `/sysroot/oem`. This label is set in grub by default (`rd.cos.oemlabel=COS_OEM`) but also on the default `cos-layout.env` file with Kairos. This partition is not mandatory so It's allowed to fail
 - `rootfs-hook`: Runs the cloud config stage `rootfs`. Notice that this runs very early in the process so things like binds or RW paths are not yet mounted
 - `load-config`: This parses the `/run/cos/cos-layout.env` file (usually generated by the `rootfs` stage) and loads all the configurations
 - `overlay-mount`: This mounts the paths set in the config (`RW_PATHS`) under the `/run/overlay` dir, so they are RW
 - `custom-mount`: This mounts the paths set in the config (`VOLUMES`) or in cmdline `rd.cos.mount=` in the given path (`LABEL=COS_PERSISTENT:/usr/local`)
 - `mount-bind`: This mounts the paths set in the config (`PERSISTENT_STATE_PATHS` and `CUSTOM_BIND_MOUNTS`) as bind mounts under the `PERSISTENT_STATE_TARGET` which defaults to `/usr/local/.state`
 - `quarantine-stale-units`: Moves unit symlinks in the persistent `/etc/systemd/system` that point at a target the current image does not ship, when the image does ship a real unit of the same name, into `/etc/systemd/kairos-stale-units`. Because `/etc/systemd` is a persistent state bind and the state sync never deletes, a symlink an earlier image enabled outlives it and shadows the packaged unit. Nothing is deleted: the symlink is only moved out of systemd's unit load path, so putting it back is one `mv`. Masks (targets pointing at `/dev/null`) and the enablement symlinks under `.wants/` and `.requires/` are left alone
 - `write-fstab`: Writes the final fstab with all the mounts into `/sysroot/fstab`
 - `initramfs-hook`: Runs the cloud config stage `initramfs`. Note that this is run under a chroot into what will be the final system (/sysroot).
 - `wait-for-sysroot`: Waits for the /sysroot and /sysroot/system dirs to be available, which means that they are mounted. Useful when booting from CD/Netboot as immucore doesn't mount the /sysroot in those cases, but we want to run the initramfs stage once the system is ready.

### UKI mode (Experimental)

---

Currently, there is experimental support to boot in UKI mode without doing a final switch_root into `/sysroot`
This means that the initramfs is not really an initramfs. Nevertheless, the final system and contains all the needed parts to boot.
This, mixed with a UKI binary in which we dump everything into the final binary, means that you can have a single EFI file with your full system.

This is currently activated by setting the `rd.immucore.uki` on the cmdline.


------

<table>
<tr>
<th align="center">
<img width="640" height="1px">
<p> 
<small>
Documentation
</small>
</p>
</th>
<th align="center">
<img width="640" height="1">
<p> 
<small>
Contribute
</small>
</p>
</th>
</tr>
<tr>
<td>

 📚 [Getting started with Kairos](https://kairos.io/docs/getting-started) <br> :bulb: [Examples](https://kairos.io/docs/examples) <br> :movie_camera: [Video](https://kairos.io/docs/media/) <br> :open_hands:[Engage with the Community](https://kairos.io/community/)
  
</td>
<td>
  
🙌[ CONTRIBUTING.md ]( https://github.com/kairos-io/kairos/blob/master/CONTRIBUTING.md ) <br> :raising_hand: [ GOVERNANCE ]( https://github.com/kairos-io/kairos/blob/master/GOVERNANCE.md ) <br>:construction_worker:[Code of conduct](https://github.com/kairos-io/kairos/blob/master/CODE_OF_CONDUCT.md) 
  
</td>
</tr>
</table>

![immucore](https://user-images.githubusercontent.com/1447686/224991389-0355a268-7600-4b4a-9b29-838480968e7a.svg)


