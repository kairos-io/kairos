#!/bin/bash
## This script is used to prepare the overlay files to be copied into /framework.
## Depending on the FLAVOR, it might be needed to overlay different files in the rootfs, and this is handled in this script.
## Note: This could be dropped in favor of unifying every configuration into cloud-config files, so we can avoid this switch here, however, 
##       this is needed until then.
set -ex

FLAVOR=$1

if [[ "$FLAVOR" == "" ]]; then
    echo "no flavor specified"
    exit 1
fi

if [[ "$FLAVOR" =~ ^alpine.* ]]; then
    cp -rfv /overlay/files-alpine/* /framework
elif [[ "$FLAVOR" = "fedora" || "$FLAVOR" = "rockylinux" ]]; then
    cp -rfv /overlay/files-fedora/* /framework
elif [[ "$FLAVOR" = "debian" || "$FLAVOR" = "ubuntu" || "$FLAVOR" = "ubuntu-20-lts" || "$FLAVOR" = "ubuntu-22-lts" ]]; then
    cp -rfv /overlay/files-ubuntu/* /framework
elif [[ "$FLAVOR" =~ ^ubuntu-.*-lts-arm-.*$ ]]; then
    cp -rfv /overlay/files-ubuntu-arm-rpi/* /framework
fi

if [[ "$FLAVOR" = "ubuntu-20-lts-arm-nvidia-jetson-agx-orin" ]]; then
    cp -rfv /overlay/files-nvidia/* /framework
fi
