# Kairos Ubuntu focal fips

- Edit `pro-attach-config.yaml` with your token
- run `bash build.sh`
- start the ISO with qemu `bash run.sh`
Install the system with a cloud-config file adding `fips=1` to the boot options:

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
- The dracut patch is needed as Ubuntu has an older version of systemd
- Most of the Dockerfile configuration are: packages being installed by ubuntu, and the framework files coming from Kairos containing FIPS-enabled packages
