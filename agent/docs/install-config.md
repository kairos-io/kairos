# Install Configuration

This document describes the `install` block in Kairos cloud-config, which drives automatic installations without user interaction.

## Overview

The `install` block is a YAML section in a `#cloud-config` file that controls how Kairos installs itself to a disk. It includes options for partitioning, encryption, kernel parameters, and cloud-config files to apply on first boot.

## Cloud-Config Files: `install.oem_files`

The `install.oem_files` key allows you to include additional cloud-config files that will be written to the installed system's cloud-config directory during installation, so they are applied on the system's first boot.

### Format

```yaml
install:
  oem_files:
    - name: 00-custom-config
      content: |
        #cloud-config
        hostname: myhost
    - name: 05-extra.yaml
      content: |
        #cloud-config
        write_files:
          - path: /etc/custom.conf
            content: custom settings
```

Each entry is an object with two required fields:

- **`name`** (string, required): A file name to write to the cloud-config directory. If the name does not end with `.yaml` or `.yml`, the `.yaml` extension is appended; if it already ends with `.yaml` or `.yml`, the name is used as-is. The name must be a single file name — path separators (`/`) and relative path components (`.`, `..`) are not allowed.
  - Examples: `00-config` → written as `/oem/00-config.yaml`, `01-extra.yaml` → written as `/oem/01-extra.yaml`

- **`content`** (string, required): A complete cloud-config document (starting with `#cloud-config`). This is the content that will be written to the file.

### Behavior

1. **Preferred location**: The files are written to the system's cloud-config directory on the OEM partition (`/oem/`) if it exists.

2. **Fallback directories**: On systems without an OEM partition (such as Raspberry Pi images flashed from prebuilt images), the files are written to the first available of:
   - `/usr/local/cloud-config`
   - `/etc/kairos`

   The fallback respects the installed system's directory structure, skipping directories that are not available (e.g., if the parent directory does not exist).

3. **Validation**: All file names are validated before any files are written, so a typo in a later entry does not leave earlier entries in an inconsistent state.

4. **File permissions**: Cloud-config files are written with mode `0400` (read-only by owner) for security, as they may contain secrets applied by yip at boot.

5. **Application order**: Files are applied in alphabetical order by their final file name (after the `.yaml` extension is added or confirmed). This is independent of the order entries appear in the `oem_files` list. The order of application matters because standard cloud-config merge semantics apply: files processed later can extend or override settings from earlier files. 
   - If your file needs to be processed before or after other cloud-config files on the system (such as Kairos-generated files like `10_ssh_hardening.yaml`), use a numeric or alphabetic prefix in your file name to control the sort order. For example: `05-custom-config.yaml` will be processed before `10_ssh_hardening.yaml`, and `15-overrides.yaml` will be processed after it.

### Example: Multi-Stage Configuration

```yaml
install:
  oem_files:
    - name: 00-base
      content: |
        #cloud-config
        hostname: edge-device
        users:
          - name: core
            groups: [wheel]
            lock_passwd: true
    - name: 01-networking
      content: |
        #cloud-config
        write_files:
          - path: /etc/systemd/network/20-eth0.network
            content: |
              [Match]
              Name=eth0
              [Network]
              DHCP=yes
    - name: 02-kubelet-config
      content: |
        #cloud-config
        write_files:
          - path: /etc/kubernetes/kubelet.conf.d/custom.conf
            content: log-level=2
```

## Related Configuration

Other install-time options include:

- **`install.encryption`**: Encrypt partitions or entire systems
- **`install.ephemeral_mounts`**: Configure ephemeral (temporary) mount points
- **`install.device`**: Specify the target installation disk
- **`install.partitions`**: Customize partition layout (size, type)

For more information on the full install block, see the [Kairos documentation](https://kairos.io/docs/).
