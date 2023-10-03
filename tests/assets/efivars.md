2 Files provided for testing efivars

efivars.fd is the compiled efivars in a format that qemu can understand
efivars.json is the original json from where the efivars.fd file was created

efivars.fd can be recreated by using `virt-fw-vars` from the package `python3-virt-firmware` and is used to manipulate
efivars files and generate new ones from templates.

Assuming the OVMF package is installed and the default firmware and efivars files are at /usr/share/OVMF you can run the following to regenerate the efivars file

```bash
virt-fw-vars -i /usr/share/OVMF/OVMF_VARS.fd --set-json efivars.json -o efivars.fd
```

This uses `/usr/share/OVMF/OVMF_VARS.fd` as the base template (is empty), loads the vars from `efivars.json` and outputs the efivars.fd file


The current efivars enables SecureBoot with the default keys and also bundles the certs for our testing, available at $ROOT/tess/keys/ and what our test UKI EFI files are signed for.