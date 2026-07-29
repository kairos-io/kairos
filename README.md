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
  <a href="https://github.com/kairos-io/immucore/issues"><img src="https://img.shields.io/github/issues/kairos-io/immucore"></a>
  <a href="https://kairos.io/docs/" target=_blank> <img src="https://img.shields.io/badge/Documentation-blue"
         alt="docs"></a>
  <img src="https://img.shields.io/badge/made%20with-Go-blue">
  <img src="https://goreportcard.com/badge/github.com/kairos-io/immucore" alt="go report card" />
</p>


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

* `rd.immucore.sysrootwait=<seconds>`: Waits for the sysroot to be mounted up to <seconds> before continuing with the boot process. This is useful when booting from CD/Netboot as immucore doesn't mount the /sysroot in those cases, but we want to run the initramfs stage once the system is ready. Sometimes dracut can be really slow and the default 1 minute of waiting is not enough. In those cases you can increase this value to wait more time. Defaults to 60s.

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


