---
title: "Booting Kairos on Nvidia Jetson ARM"
linkTitle: "Booting Kairos on Nvidia Jetson ARM"
weight: 5
date: 2022-11-13
description: >
    This page contains a reference on how to run Kairos on Nvidia Jetson ARM
---

{{% alert title="Note" %}}
Please note that the following page contains only development reference. At the time of writing, we have tried porting Kairos to Jetson Nano eMMC without success. This is due to the old kernel supported (4.9), not properly working with `EFISTUB` and `U-boot` (you can see the [issue here](https://github.com/kairos-io/kairos/issues/45)). However, the steps outlined _should_ be a good reference to port Kairos to those architecture _when_ a new kernel version is available. We have tested, and have successfully booted a Jetson Nano with the 5.15 kernel, however, due to the lack of driver support, eMMC partitions are not properly recognized.
{{% /alert %}}

This page is a development reference in order to boot Kairos in Nvidia Jetson devices. Nvidia Jetson images by default ship `extlinux` as bootloader, without EFI boot. This guide explains how to get instead u-boot to chainload to `grub2`, which can be used to boot and load `Kairos`.

Note that currently there are no official Kairos core images for Jetson images, this page will refer to Jetson Nano eMMC version as the current reference, but the steps should be similar, as outline how to use the Nvidia SDK to flash the OS onboard in the eMMC of the device.

The steps involved are:

- Prepare the Kernel (if you have one, compatible with `EFISTUB`, you can skip this part)
- Flash u-boot (If the U-boot version support booting efi shells, you might skip this part too)
- Prepare the Kairos partitions
- Flash the image to the board

## Prerequisites

You need the Nvidia SDK and few other dependencies in the system. Note that for the Jetson Nano you can't use the latest SDK version as it is not anymore supporting it. The latest version available with support for Jetson Nano is [r32.7.3](https://developer.nvidia.com/embedded/linux-tegra-r3273):

```bash
# Build dependencies
apt update && apt install -y git-core build-essential bc wget xxd kmod flex libelf-dev bison libssl-dev

mkdir build
build_dir=$PWD/build
cd build

# Get Jetson SDK compatible with Jetson NANO

wget https://developer.nvidia.com/downloads/remetpack-463r32releasev73t210jetson-210linur3273aarch64tbz2 -O Jetson-210_Linux_R32.7.3_aarch64.tbz2
tar xvf Jetson-210_Linux_R32.7.3_aarch64.tbz2
```

## Prepare the Kernel

The only requirement of the kernel in order to this to work is that has to have `CONFIG_EFI_STUB` and `CONFIG_EFI` enabled.

The default kernel with the Nvidia Jetson Nano is `4.9` and it turns out to not have those enabled.

### Build from official Nvidia sources

If your kernel is not compiled to boot as _EFI stub_ you can refer to the steps below to compile the official Nvidia kernel with `EFISTUB`:

```bash
cd build
wget https://developer.nvidia.com/downloads/remack-sdksjetpack-463r32releasev73sourcest210publicsourcestbz2 -O public_sources.tbz2
wget https://developer.nvidia.com/embedded/dlc/l4t-gcc-7-3-1-toolchain-64-bit
tar xvf https://developer.nvidia.com/embedded/dlc/l4t-gcc-7-3-1-toolchain-64-bit
# gcc-linaro-7.3.1-2018.05-x86_64_aarch64-linux-gnu/....
export CROSS_COMPILE_AARCH64_PATH=$PWD/gcc-linaro-7.3.1-2018.05-x86_64_aarch64-linux-gnu/

cd Linux_for_Tegra/source/public
tar xvf kernel_src.bz2
mkdir kernel_out
echo "CONFIG_EFI_STUB=y" >> ./kernel/kernel-4.9/arch/arm64/configs/tegra_defconfig
echo "CONFIG_EFI=y" >> ./kernel/kernel-4.9/arch/arm64/configs/tegra_defconfig

# https://forums.developer.nvidia.com/t/kernel-build-script-nvbuild-sh-with-output-dir-option-not-working/173087
sed -i '86s/.*/ O_OPT=(O="${KERNEL_OUT_DIR}")/' nvbuild.sh
## See workaround for DTB errors in Troubleshooting (edit Kconfig.include..)
./nvbuild.sh -o $PWD/kernel_out
```

Note that, with the Jetson NANO, the kernel will fail to boot allocating the memory during the EFI stub boot phase.

### Build from official linux kernel

Seems the kernel `5.15` boots fine on the Jetson Nano, however, it fails to load eMMC drivers to detect eMMC partitions. A configuration reference can be found [here](https://github.com/kairos-io/packages/blob/main/packages/kernels/linux-tegra/config).

```bash
build_dir=$PWD/build
cd build

# Clone the kernel
git clone --branch v5.15 --depth 1 https://github.com/torvalds/linux.git kernel-4.9

wget https://developer.nvidia.com/downloads/remack-sdksjetpack-463r32releasev73sourcest210publicsourcestbz2 -O public_sources.tbz2
tar xvf public_sources.tbz2
wget https://developer.nvidia.com/embedded/dlc/l4t-gcc-7-3-1-toolchain-64-bit
tar xvf l4t-gcc-7-3-1-toolchain-64-bit

# Replace the kernel in the SDK
pushd Linux_for_Tegra/source/public && tar xvf kernel_src.tbz2 && rm -rf kernel/kernel-4.9 && mv $build_dir/kernel-4.9 ./kernel/ && popd

# Use the tegra config, patch nvbuild.sh
mkdir kernel_out && \
wget https://raw.githubusercontent.com/kairos-io/packages/main/packages/kernels/linux-tegra/config -O ./kernel/kernel-4.9/arch/arm64/configs/defconfig && \
wget https://raw.githubusercontent.com/kairos-io/packages/main/packages/kernels/linux-tegra/nvbuild.sh -O nvbuild.sh && chmod +x nvbuild.sh

# gcc 12 patches
pushd Linux_for_Tegra/source/public/kernel/kernel-4.9 && curl -L https://raw.githubusercontent.com/kairos-io/packages/main/packages/kernels/linux-tegra/patch.patch | patch -p1 && popd

# Build the kernel
pushd Linux_for_Tegra/source/public && \
   CROSS_COMPILE_AARCH64_PATH=$build_dir/gcc-linaro-7.3.1-2018.05-x86_64_aarch64-linux-gnu/ ./nvbuild.sh -o $PWD/kernel_out
```

## Prepare container image (Kairos)

Now we need a container image with the OS image. The image need to contain the kernel and the initramfs generated with `dracut`.

For instance, given that the kernel is available at `/boot/Image`, and the modules at `/lib/modules`:

```Dockerfile
FROM ....

RUN ln -sf Image /boot/vmlinuz
RUN kernel=$(ls /lib/modules | head -n1) && \
    dracut -f "/boot/initrd-${kernel}" "${kernel}" && \
    ln -sf "initrd-${kernel}" /boot/initrd && \
    depmod -a "${kernel}"
```

## Flashing

In order to flash to the `eMMC` we need the Nvidia SDK.

```bash
mkdir work
cd work
wget https://developer.nvidia.com/downloads/remetpack-463r32releasev73t210jetson-210linur3273aarch64tbz2
tar xvf Jetson-210_Linux_R32.7.3_aarch64.tbz2
```

### Replace U-boot (optional)

If the version of `u-boot` is old and doesn't support EFI booting, you can replace the `u-boot` binary like so:

```bash
wget http://download.opensuse.org/ports/aarch64/tumbleweed/repo/oss/aarch64/u-boot-p3450-0000-2023.01-2.1.aarch64.rpm
mkdir u-boot
cd u-boot
rpm2cpio ../u-boot-p3450-0000-2023.01-2.1.aarch64.rpm | cpio -idmv
cd ..
cd Linux_for_Tegra
# "p3450-0000" Depends on your board
cp -rfv ../u-boot/boot/u-boot.bin bootloader/t210ref/p3450-0000/u-boot.bin
```

### Disable Extlinux

We need to disable extlinux, in order for u-boot to scan for EFI shells:

```bash
# Drop extlinux
echo "" > ./bootloader/extlinux.conf
```

### Prepare Partitions

We need to prepare the partitions from the container image we want to boot, in order to achieve this, we can use `osbuilder`, which will prepare the `img` files ready to be flashed for the SDK:

```bash
cd Linux_for_Tegra
docker run --privileged -e container_image=$IMAGE -v $PWD/bootloader:/bootloader --entrypoint /prepare_arm_images.sh -ti --rm quay.io/kairos/osbuilder-tools
```

This command should create `efi.img`, `oem.img`, `persistent.img`, `recovery_partition.img`, `state_partition.img` in the `bootloader` directory

### Configure the SDK

In order to flash the partitions to the eMMC of the board, we need to configure the SDK to write the partitions to the board via its configuration files.

For the Jetson Nano, the configuration file for the partitions is located at `bootloader/t210ref/cfg/flash_l4t_t210_emmc_p3448.xml`, where we replace the `partition name=APP` with:

```xml
 <partition name="esp" type="data">
            <allocation_policy> sequential </allocation_policy>
            <filesystem_type> basic </filesystem_type>
	    <size> 20971520 </size>
	    <file_system_attribute> 0 </file_system_attribute>
	    <partition_type_guid> C12A7328-F81F-11D2-BA4B-00A0C93EC93B </partition_type_guid>
	    <allocation_attribute> 0x8 </allocation_attribute>
	    <percent_reserved> 0 </percent_reserved>
            <filename> efi.img </filename>
            <description> **Required.** Contains a redundant copy of CBoot. </description>
        </partition>
        
       <partition name="COS_RECOVERY" type="data">
            <allocation_policy> sequential </allocation_policy>
            <filesystem_type> basic </filesystem_type>
            <size> 2298478592 </size>
            <allocation_attribute>  0x8 </allocation_attribute>
            <filename> recovery_partition.img </filename>
            <description>  </description>
        </partition>
        <partition name="COS_STATE" type="data">
            <allocation_policy> sequential </allocation_policy>
            <filesystem_type> basic </filesystem_type>
            <size> 5234491392 </size>
            <allocation_attribute>  0x8 </allocation_attribute>
            <filename> state_partition.img </filename>
            <description>  </description>
        </partition>
        <partition name="COS_OEM" type="data">
            <allocation_policy> sequential </allocation_policy>
            <filesystem_type> basic </filesystem_type>
            <size> 67108864 </size>
            <allocation_attribute>  0x8 </allocation_attribute>
            <filename> oem.img </filename>
            <description>  </description>
        </partition>
        <partition name="COS_PERSISTENT" type="data">
            <allocation_policy> sequential </allocation_policy>
            <filesystem_type> basic </filesystem_type>
            <size> 2147483648 </size>
            <allocation_attribute>  0x8 </allocation_attribute>
            <filename> persistent.img </filename>
            <description>  </description>
        </partition>
```

Note: The order matters here. We want to replace the default "APP" partition with our set of partitions.

If you didn't changed the default size of the images you should be fine, however, you should check the `<size></size>` of each of the blocks if corresponds to the files generated from your container image:

```bash
stat -c %s bootloader/efi.img
stat -c %s bootloader/recovery_partition.img
stat -c %s bootloader/state_partition.img
stat -c %s bootloader/oem.img
stat -c %s bootloader/persistent.img
```

### Flash

Turn the board in recovery mode, depending on the model this process might differ:
- Turn off the board
- Jump the FCC REC pin to ground
- Plug the USB cable
- Power on the board

If you see the board ready to be flashed, you should see the following:

```bash
$ lsusb
Bus 003 Device 092: ID 0955:7f21 NVIDIA Corp. APX
```

To flash the configuration to the board, run:

```bash
./flash.sh -r jetson-nano-devkit-emmc mmcblk0p1
```

## Troubleshooting notes

You can use `picom` to see the serial console:

```bash
picocom -b 115200 /dev/ttyUSB0
```

## References

- https://docs.nvidia.com/jetson/archives/r35.1/DeveloperGuide/text/SD/SoftwarePackagesAndTheUpdateMechanism.html#update-with-partition-layout-changes
- https://docs.nvidia.com/jetson/archives/r34.1/DeveloperGuide/text/SD/Kernel/KernelCustomization.html?highlight=kernel
- https://en.opensuse.org/HCL:Jetson_Nano#Update_Firmware
- https://nullr0ute.com/2020/11/installing-fedora-on-the-nvidia-jetson-nano/
- https://forums.developer.nvidia.com/t/support-nano-on-openwrt/219168/7