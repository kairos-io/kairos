#!/usr/bin/env bash

set -e

# This script:
# - Builds the uki artifacts
# - Measures the artifacts and signs them while converting them to an UKI EFI
# - Builds the ISO
# This needs to work:
# - earthly for our artifacts to be properly generated
# - docker to sign the artifacts (Cant use earthly as it needs access to a tpm device and earhtly still doesnt allow mounts) and build the iso
# Last 2 should be easy to move into a docker container if needed and run everything in a docker container to avoid host pollution
# systemd-ukify does the following:
# - Measure the kernel, initrd, osrelease, cmdline, uname
# - insert the measurements into the UKI EFI
# - sign the UKI EFI
# HINT: consider using the ukify python file directly from https://raw.githubusercontent.com/systemd/systemd/main/src/ukify/ukify.py
# as it has a lot of fixes and changes that havent trickle down to the systemd-ukify package yet
# The iso bundles the following:
# - UKI EFI
# - systemd-boot as a bootloader, signed with our keys
# - UKI EFI public certs from kairos to add to the bootloader
# This has been tested with:
# - secureboot enabled -> doesnt work as we dont have the keys
# - secureboot disabled -> works
# - secureboot enabled with custom keys -> works
# - secureboot disabled with custom keys -> works
# It has been observed that during boot, the initrd is measured and the measurement is stored in the TPM under PCR9 under QEMU
# No other tests have been done to confirm that the measurements are being stored in the TPM or are correct




printf "Generate an uki iso based on the latest artifacts\n"
echo "------------------------------------------------------------------------"
printf "Values can be set by setting the following environment variables:\n"
printf "FLAVOR -> Flavor to build. Defaults to opensuse\n"
printf "RELEASE -> Release to use. Defaults to leap-15.5\n"
printf "FAMILY -> Family to use. Defaults to opensuse\n"
printf "VARIANT -> Variant. Between core and standard. Defaults to standard\n"
printf "MODEL -> Model. Defaults to generic\n"
printf "BASE_IMAGE -> Base image. Defaults to opensuse/leap:15.5\n"
printf "IMMUCORE_DEV -> Use immucore dev version. Defaults to true\n"
printf "IMMUCORE_DEV_BRANCH -> Branch to use from immucore repo. Defaults to master\n"
printf "BUILD_ARTIFACTS -> Use earthly to generate the uki artifacts. Useful if you got the artifacts already generated under build/.Defaults to false \n"
printf "CREATE_ISO -> Sign the artifacts and build the iso. Defaults to false\n"
printf "Most of these values can be obtained by checking the .github/flavors.yml file\n"
printf "\n"



FLAVOR=${FLAVOR:-opensuse}
RELEASE=${RELEASE:-leap-15.5}
FAMILY=${FAMILY:-opensuse}
VARIANT=${VARIANT:-core}
MODEL=${MODEL:-generic}
BASE_IMAGE=${BASE_IMAGE:-opensuse/leap:15.5}
IMMUCORE_DEV=${IMMUCORE_DEV:-true}
IMMUCORE_DEV_BRANCH=${IMMUCORE_DEV_BRANCH:-kcrypt_uki}
KAIROS_AGENT_DEV=${KAIROS_AGENT_DEV:-true}
KAIROS_AGENT_DEV_BRANCH=${KAIROS_AGENT_DEV_BRANCH:-kcrypt_uki}
BUILD_ARTIFACTS=${BUILD_ARTIFACTS:-false}
CREATE_ISO=${CREATE_ISO:-false}


echo "###############################################"
echo "Building uki iso with the following options:"
echo "FLAVOR: $FLAVOR"
echo "RELEASE: $RELEASE"
echo "FAMILY: $FAMILY"
echo "VARIANT: $VARIANT"
echo "MODEL: $MODEL"
echo "BASE_IMAGE: $BASE_IMAGE"
echo "IMMUCORE_DEV: $IMMUCORE_DEV"
echo "IMMUCORE_DEV_BRANCH: $IMMUCORE_DEV_BRANCH"
echo "KAIROS_AGENT_DEV: $KAIROS_AGENT_DEV"
echo "KAIROS_AGENT_DEV_BRANCH: $KAIROS_AGENT_DEV_BRANCH"
echo "BUILD_ARTIFACTS: $BUILD_ARTIFACTS"
echo "CREATE_ISO: $CREATE_ISO"
echo "###############################################"
printf "\n"


# Build the artifacts
if [ "$BUILD_ARTIFACTS" = true ]; then
  echo "Building artifacts"
  earthly +uki-artifacts --FLAVOR="$FLAVOR" --FLAVOR_RELEASE="$RELEASE" --FAMILY="$FAMILY" --VARIANT="$VARIANT" --MODEL="$MODEL" --BASE_IMAGE="$BASE_IMAGE" --IMMUCORE_DEV="$IMMUCORE_DEV" --IMMUCORE_DEV_BRANCH="$IMMUCORE_DEV_BRANCH" --KAIROS_AGENT_DEV="$KAIROS_AGENT_DEV" --KAIROS_AGENT_DEV_BRANCH="$KAIROS_AGENT_DEV_BRANCH"
else
  echo "Not building artifacts"
fi

if [ "$CREATE_ISO" = true ]; then
  echo "Signing EFI and creating ISO"
  test -f build/Kernel
  test -f build/Initrd
  test -f build/Cmdline
  test -f build/Osrelease
  test -f build/Uname
  test -f tests/keys/DB.key
  test -f tests/keys/DB.crt
  test -f tests/keys/private.pem
  docker run --privileged -w /workspace -v /dev:/dev -v /var/run/dbus/system_bus_socket:/var/run/dbus/system_bus_socket -v $(pwd):/workspace fedora:39 /bin/bash -exc "\
  dnf install -y binutils xorriso systemd-boot mtools efitools dosfstools sbsigntools shim openssl systemd-ukify && \
  /usr/lib/systemd/ukify build/Kernel build/Initrd \
    --cmdline @build/Cmdline \
    --os-release @build/Osrelease \
    --uname $(cat build/Uname) \
    --stub /usr/lib/systemd/boot/efi/linuxx64.efi.stub \
    --secureboot-private-key tests/keys/DB.key \
    --secureboot-certificate tests/keys/DB.crt \
    --pcr-private-key tests/keys/private.pem \
    --measure \
    --output build/uki.signed.efi && \
  sbsign --key tests/keys/DB.key --cert tests/keys/DB.crt --output build/systemd-bootx64.signed.efi /usr/lib/systemd/boot/efi/systemd-bootx64.efi && \
  mkdir -p /tmp/efi/ && \
  printf 'title Kairos %s %s\nefi /EFI/kairos/kairos.efi' ${FLAVOR} ${VERSION} > build/kairos.conf && \
  printf 'default kairos.conf' > build/loader.conf && \
  dd if=/dev/zero of=/tmp/efi/efiboot.img bs=1G count=1 && \
  mkfs.msdos -F 32 /tmp/efi/efiboot.img && \
  mmd -i /tmp/efi/efiboot.img ::EFI && \
  mmd -i /tmp/efi/efiboot.img ::EFI/BOOT && \
  mmd -i /tmp/efi/efiboot.img ::EFI/kairos && \
  mmd -i /tmp/efi/efiboot.img ::EFI/tools && \
  mmd -i /tmp/efi/efiboot.img ::loader && \
  mmd -i /tmp/efi/efiboot.img ::loader/entries && \
  mmd -i /tmp/efi/efiboot.img ::loader/keys && \
  mmd -i /tmp/efi/efiboot.img ::loader/keys/kairos && \
  mcopy -i /tmp/efi/efiboot.img tests/keys/PK.der ::loader/keys/kairos/PK.der && \
  mcopy -i /tmp/efi/efiboot.img tests/keys/KEK.der ::loader/keys/kairos/KEK.der && \
  mcopy -i /tmp/efi/efiboot.img tests/keys/DB.der ::loader/keys/kairos/DB.der && \
  mcopy -i /tmp/efi/efiboot.img build/kairos.conf ::loader/entries/kairos.conf && \
  mcopy -i /tmp/efi/efiboot.img build/loader.conf ::loader/loader.conf && \
  mcopy -i /tmp/efi/efiboot.img build/uki.signed.efi ::EFI/kairos/kairos.EFI && \
  mcopy -i /tmp/efi/efiboot.img build/systemd-bootx64.signed.efi ::EFI/BOOT/BOOTX64.EFI && \
  xorriso -as mkisofs -V 'UKI_ISO_INSTALL' -e efiboot.img -no-emul-boot -o build/uki.iso /tmp/efi
  "
else
  echo "Not signing EFI or building ISO"
fi


