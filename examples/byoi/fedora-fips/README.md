# Kairos Fedora fips

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
    extra_cmdline: "fips=1 selinux=0"
```

Notes:
- Most of the Dockerfile configuration are: packages being installed by fedora, and the framework files coming from Kairos containing FIPS-enabled packages
- The LiveCD is not running in fips mode
- You must add `selinux=0`. SELinux is not supported yet and must be explicitly disabled

## Verify FIPS is enabled

After install, you can verify that fips is enabled by running:

```bash
kairos@localhost:~$ cat /proc/sys/crypto/fips_enabled
1
kairos@localhost:~$ uname -a
Linux localhost 5.4.0-1007-fips #8-Ubuntu SMP Wed Jul 29 21:42:48 UTC 2020 x86_64 x86_64 x86_64 GNU/Linux
```
