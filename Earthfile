VERSION 0.6
FROM alpine
ARG REGISTRY_AND_ORG=quay.io/kairos
ARG IMAGE
ARG SUPPORT=official # not using until this is defined in https://github.com/kairos-io/kairos/issues/1527
ARG GITHUB_REPO=kairos-io/kairos
# renovate: datasource=docker depName=quay.io/luet/base
ARG LUET_VERSION=0.35.0
# renovate: datasource=docker depName=aquasec/trivy
ARG TRIVY_VERSION=0.49.1
# renovate: datasource=github-releases depName=kairos-io/kairos-framework
ARG KAIROS_FRAMEWORK_VERSION=v2.7.14
ARG COSIGN_SKIP=".*quay.io/kairos/.*"
# TODO: rename ISO_NAME to something like ARTIFACT_NAME because there are place where we use ISO_NAME to refer to the artifact name

IF [ "$FLAVOR" = "ubuntu" ]
    ARG COSIGN_REPOSITORY=raccos/releases-orange
ELSE
    ARG COSIGN_REPOSITORY=raccos/releases-teal
END
ARG COSIGN_EXPERIMENTAL=0
ARG CGO_ENABLED=0
# renovate: datasource=docker depName=quay.io/kairos/osbuilder-tools versioning=semver-coerced
ARG OSBUILDER_VERSION=v0.200.4
ARG OSBUILDER_IMAGE=quay.io/kairos/osbuilder-tools:$OSBUILDER_VERSION
ARG GOLINT_VERSION=1.52.2
# renovate: datasource=docker depName=golang
ARG GO_VERSION=1.20
# renovate: datasource=docker depName=hadolint/hadolint versioning=docker
ARG HADOLINT_VERSION=2.12.0-alpine
# renovate: datasource=docker depName=renovate/renovate versioning=docker
ARG RENOVATE_VERSION=37
# renovate: datasource=docker depName=koalaman/shellcheck-alpine versioning=docker
ARG SHELLCHECK_VERSION=v0.9.0

ARG IMAGE_REPOSITORY_ORG=quay.io/kairos

ARG K3S_VERSION

all:
  ARG SECURITY_SCANS=true

  ARG TARGETARCH
  ARG --required FAMILY # The dockerfile to use
  ARG --required FLAVOR # The distribution E.g. "ubuntu"
  ARG --required FLAVOR_RELEASE # The distribution release/version E.g. "20.04"
  ARG --required VARIANT
  ARG --required MODEL
  ARG --required BASE_IMAGE # BASE_IMAGE is the image to apply the strategy (aka FLAVOR) on. E.g. ubuntu:20.04

  BUILD +base-image
  IF [ "$SECURITY_SCANS" = "true" ]
      BUILD +image-sbom
      BUILD +trivy-scan
      BUILD +grype-scan
  END
  BUILD +iso
  BUILD +netboot
  BUILD +ipxe-iso

# For PR building, only image and iso are needed
ci:
  ARG SECURITY_SCANS=true

  # args for base-image target
  ARG --required FLAVOR
  ARG --required FLAVOR_RELEASE
  ARG --required BASE_IMAGE
  ARG --required MODEL
  ARG --required VARIANT
  ARG --required FAMILY

  BUILD +base-image
  IF [ "$SECURITY_SCANS" = "true" ]
    BUILD +image-sbom
    BUILD +trivy-scan
    BUILD +grype-scan
  END
  BUILD +iso

all-arm:
  ARG --required FLAVOR
  ARG --required FLAVOR_RELEASE
  ARG --required BASE_IMAGE
  ARG --required MODEL
  ARG --required VARIANT
  ARG --required FAMILY

  ARG COMPRESS_IMG=true
  ARG SECURITY_SCANS=true

  BUILD --platform=linux/arm64 +base-image
  IF [ "$SECURITY_SCANS" = "true" ]
      BUILD --platform=linux/arm64 +image-sbom
      BUILD --platform=linux/arm64 +trivy-scan
      BUILD --platform=linux/arm64 +grype-scan
  END

  IF [ "$MODEL" = "nvidia-jetson-agx-orin" ]
    BUILD +prepare-arm-image
  ELSE
    BUILD +arm-image
  END

arm-container-image:
  BUILD --platform=linux/arm64 +base-image

all-arm-generic:
  ARG --required FLAVOR
  ARG --required FLAVOR_RELEASE
  ARG --required BASE_IMAGE
  ARG --required VARIANT
  ARG --required FAMILY
  BUILD --platform=linux/arm64 +iso --MODEL=generic

build-and-push-golang-testing:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    # Enable backports repo for debian for swtpm
    RUN . /etc/os-release && echo "deb http://deb.debian.org/debian $VERSION_CODENAME-backports main contrib non-free" > /etc/apt/sources.list.d/backports.list
    RUN apt update
    RUN apt install -y qemu-system-x86 qemu-utils git swtpm && apt clean
    SAVE IMAGE --push $IMAGE_REPOSITORY_ORG/golang-testing:${GO_VERSION}

go-deps-test:
    ARG GO_VERSION
    FROM $IMAGE_REPOSITORY_ORG/golang-testing:$GO_VERSION
    WORKDIR /build
    COPY tests/go.mod tests/go.sum ./
    RUN go mod download
    SAVE ARTIFACT go.mod go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum go.sum AS LOCAL go.sum

uuidgen:
    FROM alpine
    RUN apk add uuidgen

    COPY . ./

    RUN echo $(uuidgen) > UUIDGEN

    SAVE ARTIFACT UUIDGEN UUIDGEN

git-version:
    FROM alpine
    RUN apk add git
    COPY . ./
    RUN git describe --always --tags --dirty > GIT_VERSION
    SAVE ARTIFACT GIT_VERSION GIT_VERSION

hadolint:
    ARG HADOLINT_VERSION
    FROM hadolint/hadolint:$HADOLINT_VERSION
    WORKDIR /images
    COPY images/Dockerfile* .
    COPY .hadolint.yaml .
    RUN ls
    RUN find . -name "Dockerfile*" -print | xargs -r -n1 hadolint

renovate-validate:
    ARG RENOVATE_VERSION
    FROM renovate/renovate:$RENOVATE_VERSION
    WORKDIR /usr/src/app
    COPY renovate.json .
    RUN renovate-config-validator

shellcheck-lint:
    ARG SHELLCHECK_VERSION
    FROM koalaman/shellcheck-alpine:$SHELLCHECK_VERSION
    WORKDIR /mnt
    COPY . .
    RUN find . -name "*.sh" -print | xargs -r -n1 shellcheck

yamllint:
    FROM cytopia/yamllint
    COPY . .
    RUN yamllint .github/workflows/

lint:
    BUILD +hadolint
    BUILD +renovate-validate
    BUILD +shellcheck-lint
    BUILD +yamllint

syft:
    FROM anchore/syft:latest
    SAVE ARTIFACT /syft syft

image-sbom:
    FROM +base-image
    WORKDIR /build
    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    COPY +syft/syft /usr/bin/syft
    RUN syft / -o json=sbom.syft.json -o spdx-json=sbom.spdx.json
    SAVE ARTIFACT /build/sbom.syft.json sbom.syft.json AS LOCAL build/${ISO_NAME}-sbom.syft.json
    SAVE ARTIFACT /build/sbom.spdx.json sbom.spdx.json AS LOCAL build/${ISO_NAME}-sbom.spdx.json

luet:
    FROM quay.io/luet/base:$LUET_VERSION
    SAVE ARTIFACT /usr/bin/luet /luet

###
### Image Build targets
###

kairos-dockerfile:
    ARG --required FAMILY
    COPY ./images .
    IF [ "$FAMILY" == "all" ]
        ARG FAMILY_LIST="alpine debian opensuse rhel ubuntu"
    ELSE
        ARG FAMILY_LIST=$FAMILY
    END
    FOR F IN $FAMILY_LIST
        RUN --no-cache cat <(echo "# This file is auto-generated with the command: earthly +kairos-dockerfile --FAMILY=${F}") \
            <(sed -n '/# WARNING:/!p' Dockerfile.$F) \
            <(echo) \
            <(sed -n '/# WARNING:/!p' Dockerfile.kairos) \
            > ./Dockerfile
        SAVE ARTIFACT Dockerfile AS LOCAL images/Dockerfile.kairos-${F}
    END


extract-framework-profile:
    ARG FRAMEWORK_VERSION
    IF [ "$FRAMEWORK_VERSION" != "" ]
        ARG _FRAMEWORK_VERSION=$FRAMEWORK_VERSION
    ELSE
        ARG _FRAMEWORK_VERSION=$KAIROS_FRAMEWORK_VERSION
    END

    FROM quay.io/kairos/framework:${_FRAMEWORK_VERSION}
    SAVE ARTIFACT /etc/luet/luet.yaml framework-profile.yaml AS LOCAL ./framework-profile.yaml

extract-kairos-agent-from-framework:
    FROM quay.io/kairos/framework:${KAIROS_FRAMEWORK_VERSION}
    SAVE ARTIFACT /usr/bin/kairos-agent kairos-agent AS LOCAL ./kairos-agent

base-image:
    ARG TARGETARCH # Earthly built-in (not passed)
    ARG --required FAMILY # The dockerfile to use
    ARG --required FLAVOR # The distribution E.g. "ubuntu"
    ARG --required FLAVOR_RELEASE # The distribution release/version E.g. "20.04"
    ARG --required VARIANT
    ARG --required MODEL
    ARG --required BASE_IMAGE # BASE_IMAGE is the image to apply the strategy (aka FLAVOR) on. E.g. ubuntu:20.04
    ARG FRAMEWORK_VERSION
    ARG BOOTLOADER=grub
    # TODO for the framework image. Do we call the last stable version available or master?

    ARG K3S_VERSION # As it comes from luet package
    ARG SOFTWARE_VERSION_PREFIX="k3s"
    ARG _SOFTWARE_LUET_VERSION=$K3S_VERSION
    # Takes 1.28.2+1 and converts that to v1.18.2+k3s1
    # Hack because we use a different version in the luet package and in the
    # artifact names.
    # TODO: Remove this when we change the package version to not have the
    # hardcoded k3s1. Then we will use the version exactly as it comes from
    # luet, in the artifact names. E.g. v1.28.2+k3s2+3 (including our build number)
    IF [ "$K3S_VERSION" != "" ]
      ARG _FIXED_VERSION=$(echo $K3S_VERSION | sed 's/+[[:digit:]]*//')
      ARG SOFTWARE_VERSION="v${_FIXED_VERSION}+k3s1"
    END

    COPY +git-version/GIT_VERSION GIT_VERSION

    ARG RELEASE=$(cat ./GIT_VERSION)

    IF [ "$FRAMEWORK_VERSION" != "" ]
        ARG _FRAMEWORK_VERSION=$FRAMEWORK_VERSION
    ELSE
        ARG _FRAMEWORK_VERSION=$KAIROS_FRAMEWORK_VERSION
    END

    FROM DOCKERFILE \
      --build-arg BASE_IMAGE=$BASE_IMAGE \
      --build-arg MODEL=$MODEL \
      --build-arg FLAVOR=$FLAVOR \
      --build-arg FLAVOR_RELEASE=$FLAVOR_RELEASE \
      --build-arg VARIANT=$VARIANT \
      --build-arg FAMILY=$FAMILY \
      --build-arg RELEASE=$RELEASE \
      --build-arg SOFTWARE_VERSION=$SOFTWARE_VERSION \
      --build-arg SOFTWARE_LUET_VERSION=$_SOFTWARE_LUET_VERSION \
      --build-arg SOFTWARE_VERSION_PREFIX=$SOFTWARE_VERSION_PREFIX \
      --build-arg FRAMEWORK_VERSION=$_FRAMEWORK_VERSION \
      --build-arg BOOTLOADER=$BOOTLOADER \
      -f +kairos-dockerfile/Dockerfile \
      ./images

    ARG _CIMG=$(cat ./IMAGE)

    COPY +git-version/GIT_VERSION VERSION
    ARG KAIROS_AGENT_DEV_BRANCH
    ARG IMMUCORE_DEV_BRANCH

    IF [ "$KAIROS_AGENT_DEV_BRANCH" != "" ]
        RUN rm -rf /usr/bin/kairos-agent
        COPY github.com/kairos-io/kairos-agent:$KAIROS_AGENT_DEV_BRANCH+build-kairos-agent/kairos-agent /usr/bin/kairos-agent
    END

    IF [ "$IMMUCORE_DEV_BRANCH" != "" ]
        RUN rm -rf /usr/bin/immucore
        COPY github.com/kairos-io/immucore:$IMMUCORE_DEV_BRANCH+build-immucore/immucore /usr/bin/immucore
        # Rebuild the initrd
        RUN if [ -f "/usr/bin/dracut" ]; then \
          kernel=$(ls /lib/modules | head -n1) && \
          dracut -f "/boot/initrd-${kernel}" "${kernel}" && \
          ln -sf "initrd-${kernel}" /boot/initrd; \
        fi
    END

    ARG _CIMG=$(cat /IMAGE)
    SAVE IMAGE $_CIMG
    SAVE ARTIFACT /IMAGE AS LOCAL build/IMAGE
    SAVE ARTIFACT VERSION AS LOCAL build/VERSION
    SAVE ARTIFACT /etc/kairos/versions.yaml versions.yaml AS LOCAL build/versions.yaml

image-rootfs:
    BUILD +base-image # Make sure the image is also saved locally
    FROM +base-image

    SAVE ARTIFACT --keep-own /. rootfs
    SAVE ARTIFACT IMAGE IMAGE


## UKI Stuff Start
uki-iso:
    ARG --required BASE_IMAGE # BASE_IMAGE is existing kairos image which needs to be converted to uki
    ARG ENKI_FLAGS
    FROM $OSBUILDER_IMAGE
    COPY ./tests/keys /keys
    WORKDIR /build
    RUN --no-cache enki build-uki $BASE_IMAGE --output-dir /build/ -k /keys --output-type iso ${ENKI_FLAGS}
    SAVE ARTIFACT /build/*.iso AS LOCAL build/

# WARNING the following targets are just for development purposes, use them at your own risk

# Base image for uki operations so we only run the install once
uki-dev-tools-image:
    FROM fedora:39
    # objcopy from binutils and systemd-stub from systemd
    RUN dnf install -y binutils systemd-boot mtools efitools sbsigntools shim openssl systemd-ukify dosfstools xorriso
    SAVE IMAGE uki-tools

# HOW TO: Generate the keys
# Platform key
# RUN openssl req -new -x509 -subj "/CN=Kairos PK/" -days 3650 -nodes -newkey rsa:2048 -sha256 -keyout PK.key -out PK.crt
# DER keys are for FW install
# RUN openssl x509 -in PK.crt -out PK.der -outform DER
# Key exchange
# RUN openssl req -new -x509 -subj "/CN=Kairos KEK/" -days 3650 -nodes -newkey rsa:2048 -sha256 -keyout KEK.key -out KEK.crt
# DER keys are for FW install
# RUN openssl x509 -in KEK.crt -out KEK.der -outform DER
# Signature DB
# RUN openssl req -new -x509 -subj "/CN=Kairos DB/" -days 3650 -nodes -newkey rsa:2048 -sha256 -keyout DB.key -out DB.crt
# DER keys are for FW install
# RUN openssl x509 -in DB.crt -out DB.der -outform DER
# But for now just use test keys pre-generated for easy testing.
# NOTE: NEVER EVER EVER use this keys for signing anything that its going outside your computer
# This is for easy testing SecureBoot locally for development purposes
# Installing this keys in other place than a VM for testing SecureBoot is irresponsible

# Base uki artifacts
# we need:
# kernel
# initramfs
# cmdline
# os-release
# uname
uki-dev-base:
    WORKDIR build
    # Build kernel,uname, etc artifacts
    FROM +base-image --BUILD_INITRD=false

    RUN /usr/bin/immucore version
    RUN /usr/bin/kairos-agent version
    RUN ln -s /usr/bin/immucore /init
    RUN mkdir -p /oem # be able to mount oem under here if found
    RUN mkdir -p /efi # mount the esp under here if found
    RUN mkdir -p /usr/local/cloud-config/ # for install/upgrade they copy stuff there
    # Put it under /tmp otherwise initramfs will contain itself. /tmp is excluded from the find
    RUN find . \( -path ./sys -prune -o -path ./run -prune -o -path ./dev -prune -o -path ./tmp -prune -o -path ./proc -prune \) -o -print | cpio -R root:root -H newc -o | gzip -2 > /tmp/initramfs.cpio.gz
    RUN echo "console=ttyS0 console=tty1 net.ifnames=1 rd.immucore.oemlabel=COS_OEM rd.immucore.debug rd.immucore.oemtimeout=2 rd.immucore.uki selinux=0" > Cmdline
    RUN basename $(ls /boot/vmlinuz-* |grep -v rescue | head -n1)| sed --expression "s/vmlinuz-//g" > Uname
    SAVE ARTIFACT /tmp/initramfs.cpio.gz initrd
    SAVE ARTIFACT Cmdline Cmdline
    SAVE ARTIFACT Uname Uname
    SAVE ARTIFACT /boot/vmlinuz Kernel
    SAVE ARTIFACT /etc/os-release Osrelease

# Now build, measure and sign the uki image
uki-dev-build:
    FROM +uki-dev-tools-image
    WORKDIR /build
    COPY tests/keys/* .
    COPY +uki-dev-base/initrd .
    COPY +uki-dev-base/Kernel .
    COPY +uki-dev-base/Cmdline .
    COPY +uki-dev-base/Uname .
    COPY +uki-dev-base/Osrelease .

    COPY +git-version/GIT_VERSION ./
    ARG KAIROS_VERSION=$(cat GIT_VERSION)

    ARG UNAME=$(cat Uname)
    RUN /usr/lib/systemd/ukify Kernel initrd \
        --cmdline=@Cmdline \
        --os-release=@Osrelease \
        --uname="${UNAME}" \
        --stub /usr/lib/systemd/boot/efi/linuxx64.efi.stub \
        --secureboot-private-key DB.key \
        --secureboot-certificate DB.crt \
        --pcr-private-key tpm2-pcr-private.pem \
        --measure \
        --output uki.signed.efi
    RUN sbsign --key DB.key --cert DB.crt --output systemd-bootx64.signed.efi /usr/lib/systemd/boot/efi/systemd-bootx64.efi
    RUN printf 'title Kairos %s\nefi /EFI/kairos/%s.efi\nversion %s' ${KAIROS_VERSION} ${KAIROS_VERSION} ${KAIROS_VERSION} > ${KAIROS_VERSION}.conf
    RUN printf 'default @saved\ntimeout 5\nconsole-mode max\neditor no\n' > loader.conf
    SAVE ARTIFACT PK.der PK.der
    SAVE ARTIFACT PK.auth PK.auth
    SAVE ARTIFACT KEK.der KEK.der
    SAVE ARTIFACT KEK.auth KEK.auth
    SAVE ARTIFACT DB.der DB.der
    SAVE ARTIFACT DB.auth DB.auth
    SAVE ARTIFACT systemd-bootx64.signed.efi systemd-bootx64.signed.efi
    SAVE ARTIFACT uki.signed.efi uki.signed.efi
    SAVE ARTIFACT ${KAIROS_VERSION}.conf ${KAIROS_VERSION}.conf
    SAVE ARTIFACT loader.conf loader.conf

# Base target to set the directory structure for the image artifacts
# as we need to create several dirs and copy files into them
# Then we generate the image from scratch to not ring anything else
uki-dev-image-artifacts:
    FROM +uki-dev-tools-image
    COPY +git-version/GIT_VERSION ./
    ARG KAIROS_VERSION=$(cat GIT_VERSION)

    COPY +uki-dev-build/systemd-bootx64.signed.efi /output/efi/EFI/BOOT/BOOTX64.EFI
    COPY +uki-dev-build/uki.signed.efi /output/efi/EFI/kairos/${KAIROS_VERSION}.efi
    COPY +uki-dev-build/${KAIROS_VERSION}.conf /output/efi/loader/entries/${KAIROS_VERSION}.conf
    COPY +uki-dev-build/loader.conf /output/efi/loader/loader.conf
    COPY +uki-dev-build/PK.der /output/efi/loader/keys/kairos/PK.der
    COPY +uki-dev-build/PK.der /output/efi/loader/keys/kairos/PK.auth
    COPY +uki-dev-build/KEK.der /output/efi/loader/keys/kairos/KEK.der
    COPY +uki-dev-build/KEK.der /output/efi/loader/keys/kairos/KEK.auth
    COPY +uki-dev-build/DB.der /output/efi/loader/keys/kairos/DB.der
    COPY +uki-dev-build/DB.der /output/efi/loader/keys/kairos/DB.auth
    SAVE ARTIFACT /output/efi efi

# This is the final artifact, only the files on it
uki-dev-image:
    COPY +base-image/IMAGE .
    ARG _CIMG=$(cat ./IMAGE)
    FROM scratch
    COPY +uki-dev-image-artifacts/efi /
    SAVE IMAGE --push $_CIMG.uki

uki-dev-iso:
    # +base-image will be called again by +uki but will be cached.
    # We just use it here to take a shortcut to the artifact name
    FROM +base-image
    WORKDIR /build
    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    COPY +git-version/GIT_VERSION ./
    ARG KAIROS_VERSION=$(cat GIT_VERSION)

    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY +uki-dev-build/systemd-bootx64.signed.efi .
    COPY +uki-dev-build/uki.signed.efi .
    COPY +uki-dev-build/${KAIROS_VERSION}.conf .
    COPY +uki-dev-build/loader.conf .
    COPY +uki-dev-build/PK.der .
    COPY +uki-dev-build/PK.auth .
    COPY +uki-dev-build/KEK.der .
    COPY +uki-dev-build/KEK.auth .
    COPY +uki-dev-build/DB.der .
    COPY +uki-dev-build/DB.auth .
    RUN mkdir -p /tmp/efi
    RUN ls -ltra /build
    # get the size of the artifacts
    ARG SIZE=$(du -sm /build | cut -f1)
    RUN ls -ltra /build
    # Create just the size we need + 50MB just in case?
    RUN dd if=/dev/zero of=/tmp/efi/efiboot.img bs=1M count=$((SIZE + 50))
    RUN mkfs.msdos -F 32 /tmp/efi/efiboot.img
    RUN mmd -i /tmp/efi/efiboot.img ::EFI
    RUN mmd -i /tmp/efi/efiboot.img ::EFI/BOOT
    RUN mmd -i /tmp/efi/efiboot.img ::EFI/kairos
    RUN mmd -i /tmp/efi/efiboot.img ::EFI/tools
    RUN mmd -i /tmp/efi/efiboot.img ::loader
    RUN mmd -i /tmp/efi/efiboot.img ::loader/entries
    RUN mmd -i /tmp/efi/efiboot.img ::loader/keys
    RUN mmd -i /tmp/efi/efiboot.img ::loader/keys/auto
    RUN mcopy -i /tmp/efi/efiboot.img PK.der ::loader/keys/auto/PK.der
    RUN mcopy -i /tmp/efi/efiboot.img PK.auth ::loader/keys/auto/PK.auth
    RUN mcopy -i /tmp/efi/efiboot.img KEK.der ::loader/keys/auto/KEK.der
    RUN mcopy -i /tmp/efi/efiboot.img KEK.auth ::loader/keys/auto/KEK.auth
    RUN mcopy -i /tmp/efi/efiboot.img DB.der ::loader/keys/auto/DB.der
    RUN mcopy -i /tmp/efi/efiboot.img DB.auth ::loader/keys/auto/DB.auth
    RUN mcopy -i /tmp/efi/efiboot.img ${KAIROS_VERSION}.conf ::loader/entries/${KAIROS_VERSION}.conf
    RUN mcopy -i /tmp/efi/efiboot.img loader.conf ::loader/loader.conf
    RUN mcopy -i /tmp/efi/efiboot.img uki.signed.efi ::EFI/kairos/${KAIROS_VERSION}.efi
    RUN mcopy -i /tmp/efi/efiboot.img systemd-bootx64.signed.efi ::EFI/BOOT/BOOTX64.EFI
    RUN xorriso -as mkisofs -V 'UKI_ISO_INSTALL' -e efiboot.img -no-emul-boot -o $ISO_NAME.iso /tmp/efi
    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.uki.iso
# Uki stuff End

###
### Artifacts targets (ISO, netboot, ARM)
###

iso:
    FROM +base-image
    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY . ./

    BUILD +image-rootfs # Make sure the image is also saved locally
    COPY --keep-own +image-rootfs/rootfs /build/image
    COPY --keep-own +image-rootfs/IMAGE IMAGE


    RUN /entrypoint.sh --name $ISO_NAME --debug build-iso --squash-no-compression --date=false dir:/build/image --output /build/
    SAVE ARTIFACT IMAGE AS LOCAL build/IMAGE
    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.iso
    SAVE ARTIFACT /build/$ISO_NAME.iso.sha256 kairos.iso.sha256 AS LOCAL build/$ISO_NAME.iso.sha256

# This target builds an iso using a remote docker image as rootfs instead of building the whole rootfs
# This should be really fast as it uses an existing image. This requires a pushed image from the +image target
# defaults to use the $REMOTE_IMG name (so ttl.sh/core-opensuse-leap:latest)
# you can override either the full thing by setting --REMOTE_IMG=docker:REPO/IMAGE:TAG
# or by --REMOTE_IMG=REPO/IMAGE:TAG
iso-remote:
    ARG --required REMOTE_IMG
    FROM $REMOTE_IMG
    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY . ./
    RUN /entrypoint.sh --name $ISO_NAME --debug build-iso --squash-no-compression --date=false docker:$REMOTE_IMG --output /build/

    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.iso
    SAVE ARTIFACT /build/$ISO_NAME.iso.sha256 kairos.iso.sha256 AS LOCAL build/$ISO_NAME.iso.sha256

netboot:
    FROM +base-image

    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    # Variables used here:
    # https://github.com/kairos-io/osbuilder/blob/66e9e7a9403a413e310f462136b70d715605ab09/tools-image/ipxe.tmpl#L5
    COPY +git-version/GIT_VERSION GIT_VERSION
    ARG VERSION=$(cat ./GIT_VERSION)
    ARG RELEASE_URL=https://github.com/kairos-io/kairos/releases/download

    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build

    COPY +iso/kairos.iso kairos.iso

    RUN isoinfo -x /rootfs.squashfs -R -i kairos.iso > ${ISO_NAME}.squashfs
    RUN isoinfo -x /boot/kernel -R -i kairos.iso > ${ISO_NAME}-kernel
    RUN isoinfo -x /boot/initrd -R -i kairos.iso > ${ISO_NAME}-initrd
    RUN envsubst >> ${ISO_NAME}.ipxe < /ipxe.tmpl

    SAVE ARTIFACT /build/$ISO_NAME.squashfs squashfs AS LOCAL build/$ISO_NAME.squashfs
    SAVE ARTIFACT /build/$ISO_NAME-kernel kernel AS LOCAL build/$ISO_NAME-kernel
    SAVE ARTIFACT /build/$ISO_NAME-initrd initrd AS LOCAL build/$ISO_NAME-initrd
    SAVE ARTIFACT /build/$ISO_NAME.ipxe ipxe AS LOCAL build/$ISO_NAME.ipxe

arm-image:
  ARG OSBUILDER_IMAGE
  ARG COMPRESS_IMG=true
  ARG IMG_COMPRESSION=xz

  FROM --platform=linux/arm64 +base-image
  ARG IMAGE_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//').img

  FROM $OSBUILDER_IMAGE
  ARG --required MODEL

  WORKDIR /build
  # These sizes are in MB
  ENV SIZE="15200"
  IF [[ "$FLAVOR" = "ubuntu" ]]
    ENV DEFAULT_ACTIVE_SIZE="2700"
    ENV STATE_SIZE="8100" # Has to be DEFAULT_ACTIVE_SIZE * 3 due to upgrade
    ENV RECOVERY_SIZE="5400" # Has to be DEFAULT_ACTIVE_SIZE * 2 due to upgrade
  ELSE
    ENV STATE_SIZE="6200"
    ENV RECOVERY_SIZE="4200"
    ENV DEFAULT_ACTIVE_SIZE="2000"
  END

  COPY --platform=linux/arm64 +image-rootfs/rootfs /build/image
  # With docker is required for loop devices
  WITH DOCKER --allow-privileged
    RUN /build-arm-image.sh --use-lvm --model $MODEL --directory "/build/image" /build/$IMAGE_NAME
  END
  IF [ "$COMPRESS_IMG" = "true" ]
    IF [ "$IMG_COMPRESSION" = "zstd" ]
      RUN zstd --rm /build/$IMAGE_NAME
      SAVE ARTIFACT /build/$IMAGE_NAME.zst img AS LOCAL build/$IMAGE_NAME.zst
    ELSE IF [ "$IMG_COMPRESSION" = "xz" ]
      RUN xz -v /build/$IMAGE_NAME
      SAVE ARTIFACT /build/$IMAGE_NAME.xz img AS LOCAL build/$IMAGE_NAME.xz
    END
  ELSE
      SAVE ARTIFACT /build/$IMAGE_NAME img AS LOCAL build/$IMAGE_NAME
  END
  SAVE ARTIFACT /build/$IMAGE_NAME.sha256 img-sha256 AS LOCAL build/$IMAGE_NAME.sha256

prepare-arm-image:
  ARG OSBUILDER_IMAGE
  ARG COMPRESS_IMG=true

  FROM $OSBUILDER_IMAGE
  WORKDIR /build

  # These sizes are in MB and are specific only for the nvidia-jetson-agx-orin
  ENV SIZE="15200"
  ENV STATE_SIZE="14000"
  ENV RECOVERY_SIZE="10000"
  ENV DEFAULT_ACTIVE_SIZE="4500"
  
  COPY --platform=linux/arm64 +image-rootfs/rootfs /build/image

  ENV directory=/build/image
  RUN mkdir bootloader
  # With docker is required for loop devices
  WITH DOCKER --allow-privileged
    RUN /prepare_arm_images.sh
  END

  SAVE ARTIFACT /build/bootloader/efi.img efi.img AS LOCAL build/efi.img
  SAVE ARTIFACT /build/bootloader/oem.img oem.img AS LOCAL build/oem.img
  SAVE ARTIFACT /build/bootloader/persistent.img persistent.img AS LOCAL build/persistent.img
  SAVE ARTIFACT /build/bootloader/recovery_partition.img recovery_partition.img AS LOCAL build/recovery_partition.img
  SAVE ARTIFACT /build/bootloader/state_partition.img state_partition.img AS LOCAL build/state_partition.img

ipxe-iso:
    ARG TARGETARCH

    FROM +base-image
    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    # Variables used here:
    # https://github.com/kairos-io/osbuilder/blob/66e9e7a9403a413e310f462136b70d715605ab09/tools-image/ipxe.tmpl#L5
    COPY +git-version/GIT_VERSION GIT_VERSION
    ARG VERSION=$(cat ./GIT_VERSION)
    ARG RELEASE_URL=https://github.com/kairos-io/kairos/releases/download

    FROM ubuntu
    ARG ipxe_script
    RUN apt update
    RUN apt install -y -o Acquire::Retries=50 \
                           mtools syslinux isolinux gcc-arm-none-eabi git make gcc liblzma-dev mkisofs xorriso
                           # jq docker
    WORKDIR /build

    RUN git clone https://github.com/ipxe/ipxe
    IF [ "$ipxe_script" = "" ]
        COPY (+netboot/ipxe --VERSION=$VERSION --RELEASE_URL=$RELEASE_URL) /build/ipxe/script.ipxe
    ELSE
        COPY $ipxe_script /build/ipxe/script.ipxe
    END
    RUN cd ipxe/src && \
        sed -i 's/#undef\tDOWNLOAD_PROTO_HTTPS/#define\tDOWNLOAD_PROTO_HTTPS/' config/general.h && \
        make EMBED=/build/ipxe/script.ipxe
    SAVE ARTIFACT /build/ipxe/src/bin/ipxe.iso iso AS LOCAL build/${ISO_NAME}-ipxe.iso
    SAVE ARTIFACT /build/ipxe/src/bin/ipxe.usb usb AS LOCAL build/${ISO_NAME}-ipxe-usb.img

# Uses the same config as in the docs: https://kairos.io/docs/advanced/build/#build-a-cloud-image
# This is the default for cloud images which only come with the recovery partition and the workflow
# is to boot from them and do a reset to get the latest system installed
# This allows us to build a raw disk image locally to test the cloud workflow easily
raw-image:
    # +base-image will be called again by +uki-artifacts but will be cached
    # We just use it here to take a shortcut to the artifact name
    FROM +base-image
    WORKDIR /build
    ARG IMG_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//').raw

    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY tests/assets/raw_image.yaml /raw_image.yaml
    COPY --keep-own +image-rootfs/rootfs /rootfs
    RUN /raw-images.sh /rootfs /$IMG_NAME /raw_image.yaml
    RUN truncate -s "+$((32000*1024*1024))" /$IMG_NAME
    SAVE ARTIFACT /$IMG_NAME $IMG_NAME AS LOCAL build/$IMG_NAME

# Generic targets
# usage e.g. ./earthly.sh +datasource-iso --CLOUD_CONFIG=tests/assets/qrcode.yaml
datasource-iso:
  ARG OSBUILDER_IMAGE
  ARG CLOUD_CONFIG
  FROM $OSBUILDER_IMAGE
  WORKDIR /build
  RUN touch meta-data
  COPY ${CLOUD_CONFIG} user-data
  RUN cat user-data
  RUN mkisofs -output ci.iso -volid cidata -joliet -rock user-data meta-data
  SAVE ARTIFACT /build/ci.iso iso.iso AS LOCAL build/datasource.iso

###
### Security target scan
###
trivy:
    ARG TRIVY_VERSION
    FROM aquasec/trivy:$TRIVY_VERSION
    SAVE ARTIFACT /contrib contrib
    SAVE ARTIFACT /usr/local/bin/trivy /trivy

trivy-scan:
    ARG TARGETARCH

    # Use base-image so it can read original os-release file
    FROM +base-image

    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    COPY +trivy/trivy /trivy
    COPY +trivy/contrib /contrib

    WORKDIR /build
    RUN /trivy filesystem --skip-dirs /tmp --timeout 30m --format sarif -o report.sarif --no-progress /
    RUN /trivy filesystem --skip-dirs /tmp --timeout 30m --format template --template "@/contrib/html.tpl" -o report.html --no-progress /
    RUN /trivy filesystem --skip-dirs /tmp --timeout 30m -f json -o results.json --no-progress /
    SAVE ARTIFACT /build/report.sarif report.sarif AS LOCAL build/${ISO_NAME}-trivy.sarif
    SAVE ARTIFACT /build/report.html report.html AS LOCAL build/${ISO_NAME}-trivy.html
    SAVE ARTIFACT /build/results.json results.json AS LOCAL build/${ISO_NAME}-trivy.json

grype:
    FROM anchore/grype
    SAVE ARTIFACT /grype /grype

grype-scan:
    ARG TARGETARCH

    # Use base-image so it can read original os-release file
    FROM +base-image
    COPY +grype/grype /grype

    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    WORKDIR /build
    RUN /grype dir:/ --output sarif --add-cpes-if-none --file report.sarif
    RUN /grype dir:/ --output json --add-cpes-if-none --file report.json
    SAVE ARTIFACT /build/report.sarif report.sarif AS LOCAL build/${ISO_NAME}-grype.sarif
    SAVE ARTIFACT /build/report.json report.json AS LOCAL build/${ISO_NAME}-grype.json


###
### Test targets
###
# usage e.g. ./earthly.sh +run-qemu-datasource-tests --FLAVOR=alpine-opensuse-leap --FROM_ARTIFACTS=true
run-qemu-datasource-tests:
    FROM +go-deps-test
    WORKDIR /test
    ARG FLAVOR
    ARG PREBUILT_ISO
    ARG TEST_SUITE=autoinstall-test
    ENV FLAVOR=$FLAVOR
    ENV SSH_PORT=60023
    ENV CREATE_VM=true
    ARG CLOUD_CONFIG="./tests/assets/autoinstall.yaml"
    ENV USE_QEMU=true

    ENV CLOUD_CONFIG=$CLOUD_CONFIG
    COPY . .
    IF [ -n "$PREBUILT_ISO" ]
        ENV ISO=/test/$PREBUILT_ISO
    ELSE
        COPY +iso/kairos.iso kairos.iso
        ENV ISO=/test/kairos.iso
    END

    RUN echo "Using iso from $ISO"

    IF [ ! -e /test/build/datasource.iso ]
        COPY ( +datasource-iso/iso.iso --CLOUD_CONFIG=$CLOUD_CONFIG) datasource.iso
        ENV DATASOURCE=/test/datasource.iso
    ELSE
        ENV DATASOURCE=/test/build/datasource.iso
    END
    ENV CLOUD_INIT=/tests/tests/$CLOUD_CONFIG
    COPY +go-deps-test/go.mod go.mod
    COPY +go-deps-test/go.sum go.sum
    RUN go run github.com/onsi/ginkgo/v2/ginkgo -v --label-filter "$TEST_SUITE" --fail-fast -r ./tests/


run-qemu-netboot-test:
    FROM +base-image
    ARG ISO_NAME=$(cat /etc/os-release | grep 'KAIROS_ARTIFACT' | sed 's/KAIROS_ARTIFACT=\"//' | sed 's/\"//')

    COPY +git-version/GIT_VERSION GIT_VERSION
    ARG VERSION=$(cat ./GIT_VERSION)

    FROM +go-deps-test
    COPY . /test
    WORKDIR /test

    # This is the IP at which qemu vm can see the host
    ARG IP="10.0.2.2"

    COPY (+netboot/squashfs --VERSION=$VERSION --RELEASE_URL=http://$IP) ./build/$VERSION/$ISO_NAME.squashfs
    COPY (+netboot/kernel --VERSION=$VERSION --RELEASE_URL=http://$IP) ./build/$VERSION/$ISO_NAME-kernel
    COPY (+netboot/initrd --VERSION=$VERSION --RELEASE_URL=http://$IP) ./build/$VERSION/$ISO_NAME-initrd
    COPY (+netboot/ipxe --VERSION=$VERSION --RELEASE_URL=http://$IP) ./build/$VERSION/$ISO_NAME.ipxe
    COPY (+ipxe-iso/iso --VERSION=$VERSION --RELEASE_URL=http://$IP) ./build/${ISO_NAME}-ipxe.iso

    ENV ISO=/test/build/$ISO_NAME-ipxe.iso

    ENV CREATE_VM=true
    ENV USE_QEMU=true
    ARG TEST_SUITE=netboot-test

    COPY +go-deps-test/go.mod go.mod
    COPY +go-deps-test/go.sum go.sum
    # TODO: use --pull or something to cache the python image in Earthly
    WITH DOCKER
        RUN docker run -d -v $PWD/build:/build --workdir=/build \
            --net=host -it python:3.11.0-bullseye python3 -m http.server 80 && \
            go run github.com/onsi/ginkgo/v2/ginkgo --label-filter "$TEST_SUITE" --fail-fast -r ./tests/
    END

run-qemu-test:
    FROM +go-deps-test
    WORKDIR /test
    ARG TEST_SUITE=upgrade-with-cli
    ARG PREBUILT_ISO
    ARG CONTAINER_IMAGE
    ENV SSH_PORT=60022
    ENV CREATE_VM=true
    ENV USE_QEMU=true

    COPY . .
    IF [ -n "$PREBUILT_ISO" ]
        ENV ISO=/test/$PREBUILT_ISO
    ELSE
        ARG --required FLAVOR
        ARG --required FLAVOR_RELEASE
        ARG --required FAMILY
        ARG --required BASE_IMAGE
        ARG --required MODEL
        ARG --required VARIANT
        COPY +iso/kairos.iso kairos.iso
        ENV ISO=/test/kairos.iso
    END
    COPY +go-deps-test/go.mod go.mod
    COPY +go-deps-test/go.sum go.sum
    RUN go run github.com/onsi/ginkgo/v2/ginkgo -v --label-filter "$TEST_SUITE" --fail-fast -r ./tests/

###
### Artifacts targets
###

## Gets the latest release artifacts for a given release
pull-release:
    FROM alpine
    RUN apk add curl wget
    RUN curl -s https://api.github.com/repos/kairos-io/kairos/releases/latest | grep "browser_download_url.*${FLAVOR}.*iso" | cut -d : -f 2,3 | tr -d \" | wget -i -
    RUN mkdir build
    RUN mv *.iso build/
    SAVE ARTIFACT build AS LOCAL build

## Pull build artifacts from BUNDLE_IMAGE (expected arg)
pull-build-artifacts:
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    COPY +uuidgen/UUIDGEN ./
    ARG UUIDGEN=$(cat UUIDGEN)
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:24h

    COPY +luet/luet /usr/bin/luet
    RUN luet util unpack $BUNDLE_IMAGE build
    SAVE ARTIFACT build AS LOCAL build

## Push build artifacts as BUNDLE_IMAGE (expected arg, common is to use ttl.sh/$(uuidgen):24h)
push-build-artifacts:
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    COPY +uuidgen/UUIDGEN ./
    ARG UUIDGEN=$(cat UUIDGEN)
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:24h

    COPY . .
    COPY +luet/luet /usr/bin/luet

    RUN cd build && tar cvf ../build.tar ./
    RUN luet util pack $BUNDLE_IMAGE build.tar image.tar
    WITH DOCKER
        RUN docker load -i image.tar && docker push $BUNDLE_IMAGE
    END

# bundles tests needs to run in sequence:
# +prepare-bundles-tests
# +run-bundles-tests
prepare-bundles-tests:
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    COPY +uuidgen/UUIDGEN ./
    ARG UUIDGEN=$(cat UUIDGEN)
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:24h
    WITH DOCKER --load $IMG=(+examples-bundle --BUNDLE_IMAGE=$BUNDLE_IMAGE)
        RUN docker push $BUNDLE_IMAGE
    END
    BUILD +examples-bundle-config --BUNDLE_IMAGE=$BUNDLE_IMAGE

run-qemu-bundles-tests:
    ARG FLAVOR
    ARG PREBUILT_ISO
    BUILD +run-qemu-datasource-tests --PREBUILT_ISO=$PREBUILT_ISO --CLOUD_CONFIG=./bundles-config.yaml --TEST_SUITE="bundles-test" --FLAVOR=$FLAVOR

###
### Examples
###
### ./earthly.sh +examples-bundle --BUNDLE_IMAGE=ttl.sh/testfoobar:8h
examples-bundle:
    ARG BUNDLE_IMAGE
    FROM DOCKERFILE -f examples/bundle/Dockerfile .
    SAVE IMAGE $BUNDLE_IMAGE

## ./earthly.sh +examples-bundle-config --BUNDLE_IMAGE=ttl.sh/testfoobar:8h
## cat bundles-config.yaml
examples-bundle-config:
    ARG BUNDLE_IMAGE
    FROM alpine
    RUN apk add gettext
    COPY . .
    RUN envsubst >> tests/assets/live-overlay.yaml < tests/assets/live-overlay.tmpl
    SAVE ARTIFACT tests/assets/live-overlay.yaml AS LOCAL bundles-config.yaml

docs:
    FROM node:19-bullseye
    ARG TARGETARCH

    # Install dependencies
    RUN apt update
    RUN apt install git
    # renovate: datasource=github-releases depName=gohugoio/hugo
    ARG HUGO_VERSION="0.110.0"
    RUN wget --quiet "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz" && \
        tar xzf hugo_extended_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz && \
        rm -r hugo_extended_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz && \
        mv hugo /usr/bin

    COPY . .
    WORKDIR ./docs

    RUN npm install postcss-cli
    RUN npm run prepare

    RUN HUGO_ENV="production" /usr/bin/hugo --gc -b "/local/" -d "public/local"
    SAVE ARTIFACT public /public AS LOCAL docs/public

## ./earthly.sh --push +temp-image --FLAVOR=ubuntu
## all same flags than the `docker` target plus
## - the EXPIRATION time, defaults to 24h
## - the NAME of the image in ttl.sh, defaults to the branch name + short sha
## the push flag is optional
##
## you will have access to an image in ttl.sh e.g. ttl.sh/add-earthly-target-to-build-temp-images-339dfc7:24h
temp-image:
    FROM alpine
    RUN apk add git
    COPY . ./

    IF [ "$EXPIRATION" = "" ]
        ARG EXPIRATION="24h"
    END

    ARG BRANCH=$(git symbolic-ref --short HEAD)
    ARG SHA=$(git rev-parse --short HEAD)
    IF [ "$NAME" = "" ]
        ARG NAME="${BRANCH}-${SHA}"
    END

    ARG TTL_IMAGE = "ttl.sh/${NAME}:${EXPIRATION}"

    # args for base-image target
    ARG --required FLAVOR
    ARG --required BASE_IMAGE
    ARG --required MODEL
    ARG --required VARIANT

    FROM +base-image
    SAVE IMAGE --push $TTL_IMAGE

last-commit-packages:
    FROM quay.io/skopeo/stable
    RUN dnf install -y jq
    WORKDIR build
    RUN skopeo list-tags docker://quay.io/kairos/packages | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' > REPO_AMD64
    RUN skopeo list-tags docker://quay.io/kairos/packages-arm64 | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' > REPO_ARM64
    SAVE ARTIFACT REPO_AMD64 REPO_AMD64
    SAVE ARTIFACT REPO_ARM64 REPO_ARM64

luet-versions:
    # args for base-image target
    ARG --required FLAVOR
    ARG --required BASE_IMAGE
    ARG --required MODEL
    ARG --required VARIANT

    FROM +base-image
    SAVE ARTIFACT /framework/etc/kairos/versions.yaml versions.yaml AS LOCAL build/versions.yaml
