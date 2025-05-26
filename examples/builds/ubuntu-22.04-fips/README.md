# Kairos Ubuntu focal fips

- Edit `pro-attach-config.yaml` with your token
- run `bash build.sh`
- start the ISO with qemu `bash run.sh`

The system is not enabling FIPS by default in kernel space. 

To Install with `fips` you need a cloud-config file similar to this one adding `fips=1` to the boot options:

```yaml
#cloud-config

install:
  # ...
  # Set grub options
  grub_options:
    # additional Kernel option cmdline to apply
    extra_cmdline: "fips=1"
```

Notes:
- The modules.fips file is needed as Ubuntu has an older version of dracut which is missing 2 modules in the initramfs.
- The LiveCD is not running in fips mode, you can enable it by appending `fips=1` to the kernel command line in the boot menu.

## Verify FIPS is enabled

After install, you can verify that fips is enabled by running:

```bash
kairos@localhost:~$ cat /proc/sys/crypto/fips_enabled
1
kairos@localhost:~$ uname -r
5.15.0-140-fips
```
