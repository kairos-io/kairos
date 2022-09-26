#!/bin/bash
# Extracts squashfs, kernel, initrd and generates a ipxe template script

ISO=$1
OUTPUT_NAME=$2
VERSION=$3

isoinfo -x /rootfs.squashfs -R -i $ISO > $OUTPUT_NAME.squashfs
isoinfo -x /boot/kernel -R -i $ISO > $OUTPUT_NAME-kernel
isoinfo -x /boot/initrd -R -i $ISO > $OUTPUT_NAME-initrd

RELEASE_URL=${RELEASE_URL:-https://github.com/kairos-io/kairos/releases/download}

cat > $OUTPUT_NAME.ipxe << EOF
#!ipxe
set version ${VERSION}
set url ${RELEASE_URL}/\${version}
set kernel $OUTPUT_NAME-kernel
set initrd $OUTPUT_NAME-initrd
set rootfs $OUTPUT_NAME.squashfs
# set config https://example.com/machine-config
# set cmdline extra.values=1
kernel \${url}/\${kernel} initrd=\${initrd} ip=dhcp rd.cos.disable root=live:\${url}/\${rootfs} netboot nodepair.enable config_url=\${config} console=tty1 console=ttyS0 \${cmdline}
initrd \${url}/\${initrd}
boot
EOF
