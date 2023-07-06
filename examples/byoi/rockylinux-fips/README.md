# Kairos Rockylinux fips

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
[root@localhost ~]# cat /proc/sys/crypto/fips_enabled
1
[root@localhost ~]# uname -a
Linux localhost 5.14.0-284.18.1.el9_2.x86_64 #1 SMP PREEMPT_DYNAMIC Thu Jun 22 17:36:46 UTC 2023 x86_64 x86_64 x86_64 GNU/Linux
```
