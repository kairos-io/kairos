
- add path for model support (i.e rpi4 has a different kernel than mainline)
- add fixes for tumbleweed versions. i.e they report a number of the version, which is the build date I think. This could give us issues if we need to add a package from version X and above
- Slim down if trusted boot is selected. For example remove dracut packages, clean up any extra kernels under /var/lib/modules/KERNEL/vmlinuz
  remove generic packages, etc.
- Remove firmwares and such for trusted boot packages (check with @mauromorales, he did the initial cleanup)
- Expand validator (current checks below):
  - checks for some binaries existance
  - checks for /boot/initrd and /boot/vmlinuz to exists
  - checks for /boot/vmlinuz to be a valid symlink that resolves
  - checks for services to be there in the proper location
  - checks for binaries inside the initd