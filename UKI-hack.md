# UKI: Unified Kernel Image


It's basically a kernel, initrd and cmdline for the kernel all lumped up together in an efi binary. Mixing it with something like systemd-stub
means that you can boot from the EFI shell directly into the system.

You can add more stuff to it like the os-release info, the kernel version (uname), splash image, Devicetree , etc...

This way you got everything in one nice package and can sign the whole thing for secureboot or calculate the hashes for measured boot.


Usually under secureboot the initrd is not signed (as its generated locally), so once the kernel is run initrd signature is not verified. Nor you can measure it with TPM PCRs

UKI bundles the kernel with initrd and everything else, so you can sign the whole thing AND pre-calculate the hashes for TPM PCRs in advance.


Good writeup: https://0pointer.net/blog/brave-new-trusted-boot-world.html


### So why not a bit more?

So why not store the whole system in the initramfs? 

In this branch on the earthfile there is a new target called uki. This will generate an efi with the whole kairos system under the initramfs.
This uses immucore to mount and set up the whole system.

There is an extra target called `prepare-uki-disk-image` which will generate a disk.img with the efi file inside in the proper place, so you
can just attach that image to a qemu vm and boot from there. An extra arg `SIGNED_EFI` will provide the same image but with a signed efi and all the keys needed
to insert hem into the uefo firmware and test secureboot.

The only special thing the target does is use objcopy to add sections to the systemd-stub pointing to the correct data:

```bash
RUN objcopy /usr/lib/systemd/boot/efi/linuxx64.efi.stub \
        --add-section .osrel=Osrelease --set-section-flags .osrel=data,readonly \
        --add-section .cmdline=Cmdline --set-section-flags .cmdline=data,readonly \
        --add-section .initrd=Initrd --set-section-flags .initrd=data,readonly \
        --add-section .uname=Uname --set-section-flags .uname=data,readonly \
        --add-section .linux=Kernel --set-section-flags .linux=code,readonly \
        $ISO_NAME.unsigned.efi \
        --change-section-vma .osrel=0x17000 \
        --change-section-vma .cmdline=0x18000 \
        --change-section-vma .initrd=0x19000 \
        --change-section-vma .uname=0x5a0ed000 \
        --change-section-vma .linux=0x5a0ee000
```

Where:
* Kernel is the kernel that will be booted.
 * Initrd is the initramfs that will be booted by the kernel. Currently, a dump of the docker-rootfs...rootfs
 * Uname the output of `uname -r` (Optional content)
 * Osrelease is the /etc/os-release file from the kairos rootfs (Optional content)
 * Cmdline is the line to be passed to the kernel (Optional content, but needed in our case)
 

Good links:

 - https://man.archlinux.org/man/systemd-stub.7
 - https://wiki.osdev.org/UEFI#UEFI_applications_in_detail
 - https://github.com/uapi-group/specifications/blob/main/specs/unified_kernel_image.md
 - https://man.archlinux.org/man/systemd-measure.1.en
 - https://manuais.iessanclemente.net/images/a/a6/EFI-ShellCommandManual.pdf
 - https://0pointer.net/blog/brave-new-trusted-boot-world.html



