#!/usr/bin/env bash

set -e

# This script:
# - Builds the uki artifacts
# - Measures the artifacts and signs them while converting them to an UKI EFI
# - Builds the ISO
# This needs to work:
# - earthly for our artifacts to be properly generated
# - docker to sign the artifacts (Cant use earthly as it needs access to a tpm device and earhtly still doesnt allow mounts)
# - xorriso to build the iso
# - mtools to copy files to the iso
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
printf "SIGN_EFI -> Sign the artifacts. Useful if you already have signed them. Defaults to false\n"
printf "BUILD_ISO -> Build the iso. Defaults to false\n"
printf "Most of these values can be obtained by checking the .github/flavors.yml file\n"
printf "\n"



FLAVOR=${FLAVOR:-opensuse}
RELEASE=${RELEASE:-leap-15.5}
FAMILY=${FAMILY:-opensuse}
VARIANT=${VARIANT:-core}
MODEL=${MODEL:-generic}
BASE_IMAGE=${BASE_IMAGE:-opensuse/leap:15.5}
IMMUCORE_DEV=${IMMUCORE_DEV:-true}
IMMUCORE_DEV_BRANCH=${IMMUCORE_DEV_BRANCH:-master}
BUILD_ARTIFACTS=${BUILD_ARTIFACTS:-false}
SIGN_EFI=${SIGN_EFI:-false}
BUILD_ISO=${BUILD_ISO:-false}


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
echo "BUILD_ARTIFACTS: $BUILD_ARTIFACTS"
echo "SIGN_EFI: $SIGN_EFI"
echo "###############################################"
printf "\n"


# Build the artifacts
if [ "$BUILD_ARTIFACTS" = true ]; then
  echo "Building artifacts"
  earthly +uki-image --FLAVOR="$FLAVOR" --FLAVOR_RELEASE="$RELEASE" --FAMILY="$FAMILY" --VARIANT="$VARIANT" --MODEL="$MODEL" --BASE_IMAGE="$BASE_IMAGE" --IMMUCORE_DEV="$IMMUCORE_DEV" --IMMUCORE_DEV_BRANCH="$IMMUCORE_DEV_BRANCH"
else
  echo "Not building artifacts"
fi

if [ "$SIGN_EFI" = true ]; then
  echo "Signing EFI"
  test -f build/Kernel
  test -f build/Initrd
  test -f build/Cmdline
  test -f build/Osrelease
  test -f build/Uname
  test -f tests/keys/DB.key
  test -f tests/keys/DB.crt
  test -f tests/keys/private.pem
  docker run --privileged -w /workspace -v /dev:/dev -v /var/run/dbus/system_bus_socket:/var/run/dbus/system_bus_socket -v $(pwd):/workspace fedora:39 /bin/bash -c "\
  dnf install -y binutils systemd-boot mtools efitools sbsigntools shim openssl systemd-ukify && \
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
  sbsign --key tests/keys/DB.key --cert tests/keys/DB.crt --output build/systemd-bootx64.signed.efi /usr/lib/systemd/boot/efi/systemd-bootx64.efi"
else
  echo "Not signing EFI"
fi


if [ "$BUILD_ISO" = true ]; then
  D=$(mktemp -d)
  OLDDIR=$(pwd)
  # Check files exist before doing anything
  echo "Building ISO"
  test -f build/uki.signed.efi
  test -f build/systemd-bootx64.signed.efi
  test -f tests/keys/DB.der
  test -f tests/keys/KEK.der
  test -f tests/keys/PK.der
  # copy needed files to temp dir
  cp tests/keys/* "$D"/
  cp build/uki.signed.efi "$D"/
  cp build/systemd-bootx64.signed.efi "$D"/
  pushd "$D" || exit
  printf "title Kairos ${FLAVOR} ${VERSION}\nefi /EFI/kairos/kairos.efi" > kairos.conf
  printf "default kairos.conf" > loader.conf
  mkdir -p efi
  dd if=/dev/zero of=efi/efiboot.img bs=1G count=1
  mkfs.msdos -F 32 efi/efiboot.img
  mmd -i efi/efiboot.img ::EFI
  mmd -i efi/efiboot.img ::EFI/BOOT
  mmd -i efi/efiboot.img ::EFI/kairos
  mmd -i efi/efiboot.img ::EFI/tools
  mmd -i efi/efiboot.img ::loader
  mmd -i efi/efiboot.img ::loader/entries
  mmd -i efi/efiboot.img ::loader/keys
  mmd -i efi/efiboot.img ::loader/keys/kairos
  # Copy keys
  mcopy -i efi/efiboot.img PK.der ::loader/keys/kairos/PK.der
  mcopy -i efi/efiboot.img KEK.der ::loader/keys/kairos/KEK.der
  mcopy -i efi/efiboot.img DB.der ::loader/keys/kairos/DB.der
  # Copy kairos efi. This dir would make system-boot autosearch and add to entries automatically /EFI/Linux/
  # but here we do it by using systemd-boot as fallback so it sets the proper efivars
  mcopy -i efi/efiboot.img kairos.conf ::loader/entries/kairos.conf
  mcopy -i efi/efiboot.img uki.signed.efi ::EFI/kairos/kairos.EFI
  # systemd-boot as bootloader
  mcopy -i efi/efiboot.img loader.conf ::loader/loader.conf
  # TODO: TARGETARCH should change the output name to BOOTAA64.EFI in arm64!
  mcopy -i efi/efiboot.img systemd-bootx64.signed.efi ::EFI/BOOT/BOOTX64.EFI
  xorriso -as mkisofs -V 'UKI_ISO_INSTALL' -e efiboot.img -no-emul-boot -o uki.iso efi/
  cp uki.iso "$OLDDIR"/build
  popd || exit
  rm -Rf "${D}"
else
  echo "Not building ISO"
fi


