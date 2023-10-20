VERSION 0.6
FROM alpine
ARG VARIANT=core # core, lite, framework
ARG FLAVOR=opensuse-leap
ARG BASE_URL=quay.io/kairos
ARG IMAGE
ARG MODEL=generic
ARG SUPPORT=official # not using until this is defined in https://github.com/kairos-io/kairos/issues/1527
ARG GITHUB_REPO=kairos-io/kairos
ARG OS_ID=kairos
ARG OS_REPO=${BASE_URL}/${VARIANT}-${FLAVOR}
ARG OS_NAME=${OS_ID}-${VARIANT}-${FLAVOR}
# renovate: datasource=docker depName=quay.io/luet/base
ARG LUET_VERSION=0.35.0
# renovate: datasource=docker depName=aquasec/trivy
ARG TRIVY_VERSION=0.45.1
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
ARG OSBUILDER_VERSION=v0.8.6
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
  BUILD +base-image
  IF [ "$SECURITY_SCANS" = "true" ]
    BUILD +image-sbom
    BUILD +trivy-scan
    BUILD +grype-scan
  END
  BUILD +iso

all-arm:
  ARG SECURITY_SCANS=true
  ARG MODEL=rpi4
  BUILD --platform=linux/arm64 +base-image --MODEL=$MODEL
  IF [ "$SECURITY_SCANS" = "true" ]
      BUILD --platform=linux/arm64 +image-sbom --MODEL=$MODEL
      BUILD --platform=linux/arm64 +trivy-scan --MODEL=$MODEL
      BUILD --platform=linux/arm64 +grype-scan --MODEL=$MODEL
  END
  
  IF [[ "$FLAVOR" = "ubuntu-20-lts-arm-nvidia-jetson-agx-orin" ]]
    BUILD +prepare-arm-image --MODEL=$MODEL --FLAVOR=${FLAVOR}
  ELSE
    BUILD +arm-image --MODEL=$MODEL
  END

arm-container-image:
  ARG MODEL
  BUILD --platform=linux/arm64 +base-image --MODEL=$MODEL

all-arm-generic:
  BUILD --platform=linux/arm64 +base-image --MODEL=generic
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

CONTAINER_IMAGE_VERSION:
  COMMAND

  ARG VERSION

  # quay.io doesn't accept "+" in the repo name
  ARG _VERSION=$(echo $VERSION | sed 's/+/-/')

  IF [ "$IMAGE" = "" ]
    # TODO: This IF block should be deleted as soon as our repository names
    # follow our conventions.
    IF [ "$VARIANT" = "standard" ]
      RUN echo ${BASE_URL}/kairos-${FLAVOR}:${_VERSION} > IMAGE
    ELSE
      RUN echo ${BASE_URL}/${VARIANT}-${FLAVOR}:${_VERSION} > IMAGE
    END
  ELSE
    RUN echo $IMAGE > IMAGE
  END

  RUN echo "${VERSION}" > VERSION

OSRELEASE:
    COMMAND
    ARG OS_ID
    ARG OS_NAME
    ARG OS_REPO
    ARG OS_VERSION
    ARG VARIANT
    ARG FLAVOR
    ARG GITHUB_REPO
    ARG BUG_REPORT_URL
    ARG HOME_URL

    COPY +version/VERSION ./
    ARG OS_LABEL=$(cat VERSION)

    # update OS-release file
    RUN sed -i -n '/KAIROS_/!p' /etc/os-release
    RUN envsubst >>/etc/os-release </usr/lib/os-release.tmpl

uuidgen:
    FROM alpine
    RUN apk add uuidgen

    COPY . ./

    RUN echo $(uuidgen) > UUIDGEN

    SAVE ARTIFACT UUIDGEN UUIDGEN

version:
    FROM alpine
    RUN apk add git

    COPY . ./

    ARG _GIT_VERSION=$(git describe --always --tags --dirty)

    # Remove luet rebuild numbers like we do here:
    # https://github.com/kairos-io/packages/blob/2fbc098d0499a0c34c587057ff8a9f00c2b7f575/packages/k8s/k3s/build.yaml#L11-L12
    IF [ "$K3S_VERSION" != "" ]
      ARG _FIXED_VERSION=$(echo $K3S_VERSION | sed 's/+[[:digit:]]*//')
      ARG _K3S_VERSION="-k3sv${_FIXED_VERSION}+k3s1"
    END

    RUN --no-cache echo ${_GIT_VERSION}${_K3S_VERSION} > VERSION

    ARG VERSION=$(cat VERSION)
    SAVE ARTIFACT VERSION VERSION

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
    ARG TARGETARCH
    # Use base-image so it can read original os-release file
    FROM +base-image
    WORKDIR /build
    ARG FLAVOR
    ARG VARIANT
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)

    IF [ "$TARGETARCH" = "arm64" ]
        ARG DISTRO=$(echo $FLAVOR | sed 's/-arm-.*//')
        ARG ISO_NAME=${OS_ID}-${VARIANT}-${DISTRO}-${TARGETARCH}-${MODEL}-${VERSION}
    ELSE
        ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    END

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

# This generates the framework base by installing luet packages generated with
# the profile-build + framework-profile.yaml file.
# Installs everything under the /framework dir and saves that as an artifact
framework:
    FROM golang:alpine

    ARG FLAVOR
    WORKDIR /build

    COPY ./profile-build /build
    COPY +luet/luet /usr/bin/luet

    RUN go mod download
    COPY framework-profile.yaml /build
    RUN go run main.go ${FLAVOR} framework-profile.yaml /framework

    RUN mkdir -p /framework/etc/kairos/
    RUN luet database --system-target /framework get-all-installed --output /framework/etc/kairos/versions.yaml

    # luet cleanup
    RUN luet cleanup --system-target /framework
    RUN rm -rf /var/luet
    RUN rm -rf /var/cache

    # COPY luet into the final framework
    # TODO: Understand why?
    COPY +luet/luet /framework/usr/bin/luet
    COPY framework-profile.yaml /framework/etc/luet/luet.yaml

    SAVE ARTIFACT --keep-own /framework/ framework

build-framework-image:
    FROM alpine
    ARG FLAVOR

    # Just in case, make sure this is valid
    IF [ "$FLAVOR" = "" ]
        ARG FLAVOR=generic
    END

    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)

    ARG _IMG="$IMAGE_REPOSITORY_ORG/framework:${VERSION}_${FLAVOR}"
    RUN echo $_IMG > FRAMEWORK_IMAGE

    SAVE ARTIFACT FRAMEWORK_IMAGE AS LOCAL build/FRAMEWORK_IMAGE

    FROM scratch

    COPY (+framework/framework --FLAVOR=$FLAVOR) /

    SAVE IMAGE --push $IMAGE_REPOSITORY_ORG/framework:${VERSION}_${FLAVOR}

base-image:
    ARG MODEL
    ARG FLAVOR
    ARG VARIANT
    ARG KAIROS_VERSION
    ARG BUILD_INITRD="true"
    ARG TARGETARCH
    # HWE is used to determine if the HWE kernel should be installed on Ubuntu LTS.
    # The default value is empty, which means the HWE kernel WILL be installed
    # if you want to disable the HWE kernel, set HWE to "-non-hwe"
    ARG HWE

    IF [ "$BASE_IMAGE" = "" ]
        # DISTRO is used to match the Linux distribution in the Dockerfile e.g. Dockerfile.ubuntu
        # This is a bit messy at the moment, but it will be sorted out when we stop including the model and the arch in
        # the flavor name e.g. ubuntu-20-lts-arm-rpi
        IF [[ "$FLAVOR" =~ ^alpine* ]] # TODO: only needed while we don't have a pure alpine
            ARG DISTRO=alpine
        ELSE IF [[ "$FLAVOR" = "ubuntu-20-lts-arm-nvidia-jetson-agx-orin" ]] # TODO: needs to still be merged on Dockerfile.ubuntu (or not?)
            ARG DISTRO=ubuntu-20-lts-arm-nvidia-jetson-agx-orin
        ELSE IF [[ "$FLAVOR" =~ "ubuntu" ]] # TODO: need to find a better way to match the flavor and the distro in the dockerfile for Ubuntu
            ARG DISTRO=ubuntu
        ELSE
            ARG DISTRO=$(echo $FLAVOR | sed 's/-arm-.*//')
        END

        # SIMPLE_FLAVOR is used to distinguish the flavor inside the Dockerfile, where it's important to make a distinction
        # between e.g. ubuntu and ubuntu-20-lts, but we don't really need to know the model and the arch since this is
        # defined using MODEL and TARGETARCH.
        ARG SIMPLE_FLAVOR=$(echo $FLAVOR | sed 's/-arm-.*//')

        FROM DOCKERFILE --build-arg MODEL=$MODEL --build-arg FLAVOR=$SIMPLE_FLAVOR --build-arg HWE=$HWE -f images/Dockerfile.$DISTRO images/
    ELSE
        FROM $BASE_IMAGE
    END

    # Includes overlay/files
    # We only support non-fips for now, they are built separatedly and pushed for other to consume, not used in our workflows
    COPY (+framework/framework --FLAVOR=generic) /
    # Avoid to accidentally push keys generated by package managers
    RUN rm -rf /etc/ssh/ssh_host_*

    # Set proper os-release file with all the info
    IF [ "$KAIROS_VERSION" = "" ]
        COPY +version/VERSION ./
        ARG VERSION=$(cat VERSION)
        RUN echo "version ${VERSION}"
        ARG OS_VERSION=${VERSION}
        RUN rm VERSION
    ELSE
        ARG OS_VERSION=${KAIROS_VERSION}
    END

    DO +OSRELEASE --HOME_URL=https://github.com/kairos-io/kairos --BUG_REPORT_URL=https://github.com/kairos-io/kairos/issues --GITHUB_REPO=kairos-io/kairos --VARIANT=${VARIANT} --FLAVOR=${FLAVOR} --OS_ID=${OS_ID} --OS_LABEL=${OS_LABEL} --OS_NAME=${OS_NAME} --OS_REPO=${OS_REPO} --OS_VERSION=${OS_VERSION}

    # Fully remove machine-id, it will be generated on first boot
    RUN rm -rf /etc/machine-id

    # TEST KAIROS-AGENT FROM BRANCH
    ARG KAIROS_AGENT_DEV
    ARG KAIROS_AGENT_DEV_BRANCH=main
    IF [ "$KAIROS_AGENT_DEV" = "true" ]
        RUN rm -rf /usr/bin/kairos-agent
        COPY github.com/kairos-io/kairos-agent:$KAIROS_AGENT_DEV_BRANCH+build-kairos-agent/kairos-agent /usr/bin/kairos-agent
    END

    # TEST IMMUCORE FROM BRANCH
    ARG IMMUCORE_DEV
    ARG IMMUCORE_DEV_BRANCH=master
    IF [ "$IMMUCORE_DEV" = "true" ]
        RUN rm -Rf /usr/lib/dracut/modules.d/28immucore
        RUN rm /etc/dracut.conf.d/10-immucore.conf
        RUN rm /etc/dracut.conf.d/02-kairos-setup-initramfs.conf || exit 0
        RUN rm /etc/dracut.conf.d/50-kairos-initrd.conf || exit 0
        COPY github.com/kairos-io/immucore:$IMMUCORE_DEV_BRANCH+build-immucore/immucore /usr/bin/immucore
        COPY github.com/kairos-io/immucore:$IMMUCORE_DEV_BRANCH+dracut-artifacts/28immucore /usr/lib/dracut/modules.d/28immucore
        COPY github.com/kairos-io/immucore:$IMMUCORE_DEV_BRANCH+dracut-artifacts/10-immucore.conf /etc/dracut.conf.d/10-immucore.conf
    END

    # TEST KCRYPT FROM BRANCH
    ARG KCRYPT_DEV
    ARG KCRYPT_DEV_BRANCH=main
    IF [ "$KCRYPT_DEV" = "true" ]
        RUN rm /usr/bin/kcrypt
        COPY github.com/kairos-io/kcrypt:$KCRYPT_DEV_BRANCH+build-kcrypt/kcrypt /usr/bin/kcrypt
    END

    ARG PROVIDER_KAIROS_BRANCH
    IF [ "$VARIANT" = "standard" ]
        DO +PROVIDER_INSTALL -PROVIDER_KAIROS_BRANCH=${PROVIDER_KAIROS_BRANCH}

        DO +INSTALL_K3S --FLAVOR=$FLAVOR

        # Redo os-release with override settings to point to provider-kairos stuff
        # in earthly 0.7 we will be able to just override VARIANT here and just run the OSRELEASE once
        # but currently on 0.6 you cant override args properly as it picks the first arg it founds
        # https://docs.earthly.dev/docs/earthfile#arg
        # Overrides GITHUB_REPO, VARIANT, OS_REPO and OS_NAME to add the kairos name in there
        # which points to the provider-kairos repo
        DO +OSRELEASE --HOME_URL=https://github.com/kairos-io/kairos \
        --BUG_REPORT_URL=https://github.com/kairos-io/kairos/issues \
        --GITHUB_REPO=kairos-io/provider-kairos --VARIANT=kairos \
        --OS_REPO=quay.io/kairos/kairos-${FLAVOR} \
        --FLAVOR=${FLAVOR} --OS_ID=${OS_ID} --OS_LABEL=${OS_LABEL} \
        --OS_NAME=${OS_ID}-kairos-${FLAVOR} \
        --OS_VERSION=${OS_VERSION}
    END

    IF [[ "$FLAVOR" =~ ^ubuntu* ]]
        # compress firmware
        RUN find /usr/lib/firmware -type f -execdir zstd --rm -9 {} \+
        # compress modules
        RUN find /usr/lib/modules -type f -name "*.ko" -execdir zstd --rm -9 {} \+
    END

    IF [ "$BUILD_INITRD" = "true" ]
      IF [ "$FLAVOR" = "debian" ]
        RUN rm -rf /boot/initrd.img-*
      END


      RUN --no-cache kernel=$(ls /lib/modules | head -n1) && depmod -a "${kernel}"

      IF [ -f "/usr/bin/dracut" ]
          # Regenerate initrd if necessary
          RUN --no-cache kernel=$(ls /lib/modules | head -n1) && dracut -f "/boot/initrd-${kernel}" "${kernel}" && ln -sf "initrd-${kernel}" /boot/initrd
      END

      IF [ -f "/sbin/mkinitfs" ]
        # Proper config files with immucore and custom initrd should already be in there installed by framework
        RUN --no-cache kernel=$(ls /lib/modules | head -n1) && mkinitfs -o /boot/initrd $kernel
      END
    END

    # Set /boot/vmlinuz pointing to our kernel so kairos-agent can use it
    # https://github.com/kairos-io/kairos-agent/blob/0288fb111bc568a1bfca59cb09f39302220475b6/pkg/elemental/elemental.go#L548   q
    IF [ "$FLAVOR" = "fedora" ] || [ "$FLAVOR" = "rockylinux" ] || [ "$FLAVOR" = "almalinux" ]
        RUN rm -rf /boot/initramfs-*
    END

    IF [ ! -e "/boot/vmlinuz" ]
        IF [ -e "/boot/vmlinuz-lts" ]
            # Alpine provides the kernel under this name
            RUN ln -sf /boot/vmlinuz-lts /boot/vmlinuz
        END
        IF [ -e "/boot/vmlinuz-rpi4" ]
            # Alpine-rpi provides the kernel under this name
            RUN ln -sf /boot/vmlinuz-rpi4 /boot/vmlinuz
        END
        # If it's an ARM flavor, we want a symlink here from zImage/Image
        # Check that its not a symlink already or grub will fail!
        IF [ -e "/boot/Image" ] && [ ! -L "/boot/Image" ]
            RUN ln -sf Image /boot/vmlinuz
        ELSE IF [ -e "/boot/zImage" ]
            IF  [ ! -L "/boot/zImage" ]
                RUN ln -sf zImage /boot/vmlinuz
            ELSE
                RUN kernel=$(ls /boot/zImage-* | head -n1) && if [ -e "$kernel" ]; then ln -sf "${kernel#/boot/}" /boot/vmlinuz; fi
            END
        ELSE
            # Debian has vmlinuz-VERSION
            RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && if [ -e "$kernel" ]; then ln -sf "${kernel#/boot/}" /boot/vmlinuz; fi
            RUN kernel=$(ls /boot/Image-* | head -n1) && if [ -e "$kernel" ]; then ln -sf "${kernel#/boot/}" /boot/vmlinuz; fi
        END
    END

    RUN rm -rf /tmp/*

    DO +CONTAINER_IMAGE_VERSION -VERSION=${OS_VERSION}
    ARG _CIMG=$(cat IMAGE)

    # luet cleanup
    RUN luet cleanup
    RUN rm -rf /var/luet

    SAVE IMAGE $_CIMG
    SAVE ARTIFACT IMAGE AS LOCAL build/IMAGE
    SAVE ARTIFACT VERSION AS LOCAL build/VERSION
    SAVE ARTIFACT /etc/kairos/versions.yaml versions.yaml AS LOCAL build/versions.yaml

image-rootfs:
    FROM +base-image
    SAVE ARTIFACT --keep-own /. rootfs

uki-artifacts:
    FROM +base-image --BUILD_INITRD=false
    RUN /usr/bin/immucore version
    RUN ln -s /usr/bin/immucore /init
    RUN mkdir -p /oem # be able to mount oem under here if found
    RUN mkdir -p /efi # mount the esp under here if found
    RUN find . \( -path ./sys -prune -o -path ./run -prune -o -path ./dev -prune -o -path ./tmp -prune -o -path ./proc -prune \) -o -print | cpio -R root:root -H newc -o | gzip -2 > /tmp/initramfs.cpio.gz
    RUN echo "console=tty1 console=ttyS0 net.ifnames=1 rd.immucore.oemlabel=COS_OEM rd.immucore.oemtimeout=2 rd.immucore.debug rd.immucore.uki selinux=0" > /tmp/Cmdline
    RUN basename $(ls /boot/vmlinuz-* |grep -v rescue | head -n1)| sed --expression "s/vmlinuz-//g" > /tmp/Uname
    SAVE ARTIFACT /boot/vmlinuz Kernel
    SAVE ARTIFACT /etc/os-release Osrelease
    SAVE ARTIFACT /tmp/Cmdline Cmdline
    SAVE ARTIFACT /tmp/Uname Uname
    SAVE ARTIFACT /tmp/initramfs.cpio.gz Initrd

# Base image for uki operations so we only run the install once
uki-tools-image:
    FROM fedora:38
    # objcopy from binutils and systemd-stub from systemd
    RUN dnf install -y binutils systemd-boot mtools efitools sbsigntools shim openssl systemd-ukify

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
uki:
    ARG TARGETARCH
    COPY +version/VERSION ./
    RUN echo "version ${VERSION}"
    ARG VERSION=$(cat VERSION)
    ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    FROM +uki-tools-image
    WORKDIR build
    COPY +uki-artifacts/Kernel Kernel
    COPY +uki-artifacts/Initrd Initrd
    COPY +uki-artifacts/Osrelease Osrelease
    COPY +uki-artifacts/Uname Uname
    COPY +uki-artifacts/Cmdline Cmdline
    ARG KVERSION=$(cat Uname)
    COPY tests/keys/* .
    RUN objcopy /usr/lib/systemd/boot/efi/linuxx64.efi.stub \
            --add-section .osrel=Osrelease --set-section-flags .osrel=data,readonly \
            --add-section .cmdline=Cmdline --set-section-flags .cmdline=data,readonly \
            --add-section .initrd=Initrd --set-section-flags .initrd=data,readonly \
            --add-section .uname=Uname --set-section-flags .uname=data,readonly \
            --add-section .linux=Kernel --set-section-flags .linux=code,readonly \
            uki.unsigned.efi \
            --change-section-vma .osrel=0x17000 \
            --change-section-vma .cmdline=0x18000 \
            --change-section-vma .initrd=0x19000 \
            --change-section-vma .uname=0x5a0ed000 \
            --change-section-vma .linux=0x5a0ee000
    # example with ukify + measure
    #RUN /usr/lib/systemd/ukify Kernel Initrd \
    #    --cmdline Cmdline \
    #    --os-release Osrelease \
    #    --uname Uname \
    #    --stub /usr/lib/systemd/boot/efi/linuxx64.efi.stub \
    #    --secureboot-private-key DB.key \
    #    --secureboot-certificate DB.crt \
    #    --sign-kernel \
    #    --pcr-private-key private.pem \
    #    --pcr-public-key public.pem \
    #    --measure \
    #    --output $ISO_NAME.signed.efi
    RUN sbsign --key DB.key --cert DB.crt --output systemd-bootx64.signed.efi /usr/lib/systemd/boot/efi/systemd-bootx64.efi
    RUN sbsign --key DB.key --cert DB.crt --output uki.signed.efi uki.unsigned.efi
    SAVE ARTIFACT PK.der PK.der
    SAVE ARTIFACT KEK.der KEK.der
    SAVE ARTIFACT DB.der DB.der
    SAVE ARTIFACT systemd-bootx64.signed.efi systemd-bootx64.efi
    SAVE ARTIFACT uki.signed.efi uki.signed.efi
    SAVE ARTIFACT uki.unsigned.efi uki.unsigned.efi

# Copy uki artifacts into local build dir
uki-local-artifacts:
    FROM +uki
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    COPY +uki/systemd-bootx64.efi systemd-bootx64.efi
    COPY +uki/uki.signed.efi uki.signed.efi
    RUN printf "title Kairos ${FLAVOR} ${VERSION}\nefi /EFI/kairos/kairos.efi" > kairos.conf
    RUN printf "default kairos.conf" > loader.conf
    SAVE ARTIFACT systemd-bootx64.efi systemd-bootx64.efi AS LOCAL build/systemd-bootx64.efi
    SAVE ARTIFACT uki.signed.efi uki.signed.efi AS LOCAL build/uki.${FLAVOR}.${VERSION}.efi
    SAVE ARTIFACT kairos.conf kairos.conf AS LOCAL build/kairos.conf
    SAVE ARTIFACT loader.conf loader.conf AS LOCAL build/loader.conf

###
### Artifacts targets (ISO, netboot, ARM)
###

iso:
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG TARGETARCH
    ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY . ./
    COPY --keep-own +image-rootfs/rootfs /build/image
    RUN /entrypoint.sh --name $ISO_NAME --debug build-iso --squash-no-compression --date=false dir:/build/image --output /build/
    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.iso
    SAVE ARTIFACT /build/$ISO_NAME.iso.sha256 kairos.iso.sha256 AS LOCAL build/$ISO_NAME.iso.sha256


iso-uki:
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG TARGETARCH
    ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY +uki/uki.signed.efi .
    COPY +uki/PK.der .
    COPY +uki/KEK.der .
    COPY +uki/DB.der .
    COPY +uki/systemd-bootx64.efi .
    # Set the name for kairos manually as otherwise it picks it from the os-release automatically
    RUN printf "title Kairos ${FLAVOR} ${VERSION}\nefi /EFI/kairos/kairos.efi" > kairos.conf
    RUN printf "default kairos.conf" > loader.conf
    RUN mkdir -p /build/efi
    # TODO: Create the img size based on the actual efi size!
    RUN dd if=/dev/zero of=/build/efi/efiboot.img bs=1G count=1
    RUN mkfs.msdos -F 32 /build/efi/efiboot.img
    RUN mmd -i /build/efi/efiboot.img ::EFI
    RUN mmd -i /build/efi/efiboot.img ::EFI/BOOT
    RUN mmd -i /build/efi/efiboot.img ::EFI/kairos
    RUN mmd -i /build/efi/efiboot.img ::EFI/tools
    RUN mmd -i /build/efi/efiboot.img ::loader
    RUN mmd -i /build/efi/efiboot.img ::loader/entries
    RUN mmd -i /build/efi/efiboot.img ::loader/keys
    RUN mmd -i /build/efi/efiboot.img ::loader/keys/kairos
    # Copy keys
    RUN mcopy -i /build/efi/efiboot.img /build/PK.der ::loader/keys/kairos/PK.der
    RUN mcopy -i /build/efi/efiboot.img /build/KEK.der ::loader/keys/kairos/KEK.der
    RUN mcopy -i /build/efi/efiboot.img /build/DB.der ::loader/keys/kairos/DB.der
    # Copy kairos efi. This dir would make system-boot autosearch and add to entries automatically /EFI/Linux/
    # but here we do it by using systemd-boot as fallback so it sets the proper efivars
    RUN mcopy -i /build/efi/efiboot.img /build/kairos.conf ::loader/entries/kairos.conf
    RUN mcopy -i /build/efi/efiboot.img /build/uki.signed.efi ::EFI/kairos/kairos.EFI
    # systemd-boot as bootloader
    RUN mcopy -i /build/efi/efiboot.img /build/loader.conf ::loader/loader.conf
    # TODO: TARGETARCH should change the output name to BOOTAA64.EFI in arm64!
    RUN mcopy -i /build/efi/efiboot.img /build/systemd-bootx64.efi ::EFI/BOOT/BOOTX64.EFI
    RUN xorriso -as mkisofs -V 'UKI_ISO_INSTALL' -e efiboot.img -no-emul-boot -o /build/$ISO_NAME.iso /build/efi/
    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.iso

# This target builds an iso using a remote docker image as rootfs instead of building the whole rootfs
# This should be really fast as it uses an existing image. This requires a pushed image from the +image target
# defaults to use the $IMAGE name (so ttl.sh/core-opensuse-leap:latest)
# you can override either the full thing by setting --REMOTE_IMG=docker:REPO/IMAGE:TAG
# or by --REMOTE_IMG=REPO/IMAGE:TAG
iso-remote:
    ARG TARGETARCH
    ARG REMOTE_IMG

    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY . ./
    RUN /entrypoint.sh --name $ISO_NAME --debug build-iso --squash-no-compression --date=false docker:$REMOTE_IMG --output /build/
    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.iso
    SAVE ARTIFACT /build/$ISO_NAME.iso.sha256 kairos.iso.sha256 AS LOCAL build/$ISO_NAME.iso.sha256

netboot:
    ARG TARGETARCH
    COPY +version/VERSION ./
    RUN echo "version ${VERSION}"
    ARG VERSION=$(cat VERSION)
    ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    ARG OSBUILDER_IMAGE
    ARG RELEASE_URL=https://github.com/kairos-io/kairos/releases/download

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
  FROM $OSBUILDER_IMAGE
  ARG MODEL=rpi4
  COPY +version/VERSION ./
  RUN echo "version ${VERSION}"
  ARG VERSION=$(cat VERSION)
  ARG DISTRO=$(echo $FLAVOR | sed 's/-arm-.*//')
  # TARGETARCH is not used here because OSBUILDER_IMAGE is not available in arm64. When this changes, then the caller
  # of this target can simply pass the desired TARGETARCH.
  ARG IMAGE_NAME=${OS_ID}-${VARIANT}-${DISTRO}-arm64-${MODEL}-${VERSION}.img
  RUN echo $IMAGE_NAME
  WORKDIR /build
  # These sizes are in MB
  ENV SIZE="15200"
  IF [[ "$FLAVOR" = "ubuntu-20-lts-arm-nvidia-jetson-agx-orin" ]]
    ENV STATE_SIZE="14000"
    ENV RECOVERY_SIZE="10000"
    ENV DEFAULT_ACTIVE_SIZE="4500"
  ELSE IF [[ "$FLAVOR" =~ ^ubuntu* ]]
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
  ARG MODEL=rpi4
  ARG IMAGE_NAME=${FLAVOR}.img
  WORKDIR /build
  # These sizes are in MB
  
  ENV SIZE="15200"

  IF [[ "$FLAVOR" = "ubuntu-20-lts-arm-nvidia-jetson-agx-orin" ]]
    ENV STATE_SIZE="14000"
    ENV RECOVERY_SIZE="10000"
    ENV DEFAULT_ACTIVE_SIZE="4500"
  ELSE IF [[ "$FLAVOR" =~ ^ubuntu* ]]
    ENV DEFAULT_ACTIVE_SIZE="2700"
    ENV STATE_SIZE="8100" # Has to be DEFAULT_ACTIVE_SIZE * 3 due to upgrade
    ENV RECOVERY_SIZE="5400" # Has to be DEFAULT_ACTIVE_SIZE * 2 due to upgrade
  ELSE
    ENV STATE_SIZE="6200"
    ENV RECOVERY_SIZE="4200"
    ENV DEFAULT_ACTIVE_SIZE="2000"
  END
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
    FROM ubuntu
    ARG ipxe_script
    RUN apt update
    RUN apt install -y -o Acquire::Retries=50 \
                           mtools syslinux isolinux gcc-arm-none-eabi git make gcc liblzma-dev mkisofs xorriso
                           # jq docker
    WORKDIR /build
    COPY +version/VERSION ./
    RUN echo "version ${VERSION}"
    ARG VERSION=$(cat VERSION)
    ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG RELEASE_URL
    RUN echo "version ${VERSION}"

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
    ARG TARGETARCH
    COPY +version/VERSION ./
    RUN echo "version ${VERSION}"
    ARG VERSION=$(cat VERSION)
    ARG IMG_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}.raw
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
    COPY +trivy/trivy /trivy
    COPY +trivy/contrib /contrib
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG FLAVOR
    ARG VARIANT
    IF [ "$TARGETARCH" = "arm64" ]
        ARG DISTRO=$(echo $FLAVOR | sed 's/-arm-.*//')
        ARG ISO_NAME=${OS_ID}-${VARIANT}-${DISTRO}-${TARGETARCH}-${MODEL}-${VERSION}
    ELSE
        ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    END
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
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG FLAVOR
    ARG VARIANT
    IF [ "$TARGETARCH" = "arm64" ]
        ARG DISTRO=$(echo $FLAVOR | sed 's/-arm-.*//')
        ARG ISO_NAME=${OS_ID}-${VARIANT}-${DISTRO}-${TARGETARCH}-${MODEL}-${VERSION}
    ELSE
        ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    END
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
    ARG TARGETARCH
    FROM +go-deps-test
    COPY . /test
    WORKDIR /test

    COPY +version/VERSION ./
    RUN echo "version ${VERSION}"
    ARG VERSION=$(cat VERSION)
    ARG ISO_NAME=${OS_ID}-${VARIANT}-${FLAVOR}-${TARGETARCH}-${MODEL}-${VERSION}
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)

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
    ARG FLAVOR
    ARG TEST_SUITE=upgrade-with-cli
    ARG PREBUILT_ISO
    ARG CONTAINER_IMAGE
    ENV CONTAINER_IMAGE=$CONTAINER_IMAGE
    ENV FLAVOR=$FLAVOR
    ENV SSH_PORT=60022
    ENV CREATE_VM=true
    ENV USE_QEMU=true

    COPY . .
    IF [ -n "$PREBUILT_ISO" ]
        ENV ISO=/test/$PREBUILT_ISO
    ELSE
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
    RUN zypper in -y jq docker
    COPY +uuidgen/UUIDGEN ./
    COPY +version/VERSION ./
    ARG UUIDGEN=$(cat UUIDGEN)
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:24h

    COPY +luet/luet /usr/bin/luet
    RUN luet util unpack $BUNDLE_IMAGE build
    SAVE ARTIFACT build AS LOCAL build

## Push build artifacts as BUNDLE_IMAGE (expected arg, common is to use ttl.sh/$(uuidgen):24h)
push-build-artifacts:
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    RUN zypper in -y jq docker
    COPY +uuidgen/UUIDGEN ./
    COPY +version/VERSION ./
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
    RUN zypper in -y jq docker
    COPY +uuidgen/UUIDGEN ./
    COPY +version/VERSION ./
    ARG UUIDGEN=$(cat UUIDGEN)
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:24h
   # BUILD +examples-bundle --BUNDLE_IMAGE=$BUNDLE_IMAGE
    ARG VERSION=$(cat VERSION)
    RUN echo "version ${VERSION}"
    WITH DOCKER --load $IMG=(+examples-bundle --BUNDLE_IMAGE=$BUNDLE_IMAGE --VERSION=$VERSION)
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
    ARG VERSION
    FROM DOCKERFILE --build-arg VERSION=$VERSION -f examples/bundle/Dockerfile .
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

    FROM +base-image
    SAVE IMAGE --push $TTL_IMAGE

generate-schema:
    FROM alpine
    COPY . ./
    COPY +version/VERSION ./
    COPY +luet/luet /usr/bin/luet
    RUN mkdir -p /etc/luet/repos.conf.d/
    RUN luet repo add kairos --yes --url quay.io/kairos/packages --type docker
    RUN luet install -y system/kairos-agent
    ARG RELEASE_VERSION=$(cat VERSION)
    RUN mkdir "docs/static/$RELEASE_VERSION"
    ARG SCHEMA_FILE="docs/static/$RELEASE_VERSION/cloud-config.json"
    RUN kairos-agent print-schema > $SCHEMA_FILE 
    SAVE ARTIFACT ./docs/static/* AS LOCAL docs/static/

last-commit-packages:
    FROM quay.io/skopeo/stable
    RUN dnf install -y jq
    WORKDIR build
    RUN skopeo list-tags docker://quay.io/kairos/packages | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' > REPO_AMD64
    RUN skopeo list-tags docker://quay.io/kairos/packages-arm64 | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' > REPO_ARM64
    SAVE ARTIFACT REPO_AMD64 REPO_AMD64
    SAVE ARTIFACT REPO_ARM64 REPO_ARM64

bump-repositories:
    FROM mikefarah/yq
    WORKDIR build
    COPY +last-commit-packages/REPO_AMD64 REPO_AMD64
    COPY +last-commit-packages/REPO_ARM64 REPO_ARM64
    ARG REPO_AMD64=$(cat REPO_AMD64)
    ARG REPO_ARM64=$(cat REPO_ARM64)
    COPY framework-profile.yaml framework-profile.yaml
    RUN yq eval ".repositories[0] |= . * { \"reference\": \"${REPO_AMD64}\" }" -i framework-profile.yaml
    RUN yq eval ".repositories[1] |= . * { \"reference\": \"${REPO_ARM64}\" }" -i framework-profile.yaml
    SAVE ARTIFACT framework-profile.yaml AS LOCAL framework-profile.yaml

luet-versions:
    FROM +base-image
    SAVE ARTIFACT /framework/etc/kairos/versions.yaml versions.yaml AS LOCAL build/versions.yaml

# Installs the needed bits for "standard" images (the provider ones)
PROVIDER_INSTALL:
    COMMAND

    ARG PROVIDER_KAIROS_BRANCH

    COPY +luet/luet /usr/bin/luet

    IF [ "$PROVIDER_KAIROS_BRANCH" = "" ] # Install with luet (released versions of the binary)
      # We don't specify a version. To bump, just change what the latest version
      # in the repository is.
      RUN luet install -y system/provider-kairos
      RUN luet database get-all-installed --output /etc/kairos/versions.yaml
    ELSE # Install from a branch
      COPY github.com/kairos-io/provider-kairos:$PROVIDER_KAIROS_BRANCH+build-kairos-agent-provider/agent-provider-kairos /system/providers/agent-provider-kairos
      RUN ln -s /system/providers/agent-provider-kairos /usr/bin/kairos
    END

# Installs k3s (for "standard" images)
INSTALL_K3S:
    COMMAND

    ARG FLAVOR

    IF [ "$K3S_VERSION" = "" ]
      RUN echo "$K3S_VERSION must be set" && exit 1
    END

    IF [ "$K3S_VERSION" = "latest" ] # Install latest using the upstream installer
      ENV INSTALL_K3S_BIN_DIR="/usr/bin"
      RUN curl -sfL https://get.k3s.io > installer.sh \
          && INSTALL_K3S_SELINUX_WARN=true INSTALL_K3S_SKIP_START="true" INSTALL_K3S_SKIP_ENABLE="true" INSTALL_K3S_SKIP_SELINUX_RPM="true" bash installer.sh \
          && INSTALL_K3S_SELINUX_WARN=true INSTALL_K3S_SKIP_START="true" INSTALL_K3S_SKIP_ENABLE="true" INSTALL_K3S_SKIP_SELINUX_RPM="true" bash installer.sh agent \
          && rm -rf installer.sh
    ELSE
      IF [[ "$FLAVOR" =~ ^alpine* ]]
        ARG _LUET_K3S=$(echo k8s/k3s-openrc@${K3S_VERSION})
      ELSE
      ARG _LUET_K3S=$(echo k8s/k3s-systemd@${K3S_VERSION})
      END
    END

    RUN luet install -y ${_LUET_K3S} utils/edgevpn utils/k9s utils/nerdctl container/kubectl utils/kube-vip
    RUN luet database get-all-installed --output /etc/kairos/versions.yaml
