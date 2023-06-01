VERSION 0.6
FROM alpine
ARG VARIANT=core # core, lite, framework
ARG FLAVOR=opensuse-leap
ARG BASE_URL=quay.io/kairos
ARG IMAGE=${BASE_URL}/${VARIANT}-${FLAVOR}:latest
ARG ISO_NAME=kairos-${VARIANT}-${FLAVOR}
# renovate: datasource=docker depName=quay.io/luet/base
ARG LUET_VERSION=0.34.0
ARG OS_ID=kairos
# renovate: datasource=docker depName=aquasec/trivy
ARG TRIVY_VERSION=0.41.0
ARG COSIGN_SKIP=".*quay.io/kairos/.*"

IF [ "$FLAVOR" = "ubuntu" ]
    ARG COSIGN_REPOSITORY=raccos/releases-orange
ELSE
    ARG COSIGN_REPOSITORY=raccos/releases-teal
END
ARG COSIGN_EXPERIMENTAL=0
ARG CGO_ENABLED=0
# renovate: datasource=docker depName=quay.io/kairos/osbuilder-tools versioning=semver-coerced
ARG OSBUILDER_VERSION=v0.7.0
ARG OSBUILDER_IMAGE=quay.io/kairos/osbuilder-tools:$OSBUILDER_VERSION
ARG GOLINT_VERSION=1.52.2
# renovate: datasource=docker depName=golang
ARG GO_VERSION=1.20
# renovate: datasource=docker depName=hadolint/hadolint versioning=docker
ARG HADOLINT_VERSION=2.12.0-alpine
# renovate: datasource=docker depName=renovate/renovate versioning=docker
ARG RENOVATE_VERSION=35
# renovate: datasource=docker depName=koalaman/shellcheck-alpine versioning=docker
ARG SHELLCHECK_VERSION=v0.9.0

ARG IMAGE_REPOSITORY_ORG=quay.io/kairos


all:
  ARG SECURITY_SCANS=true
  BUILD +image
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
  BUILD +image
  IF [ "$SECURITY_SCANS" = "true" ]
    BUILD +image-sbom
    BUILD +trivy-scan
    BUILD +grype-scan
  END
  BUILD +iso

all-arm:
  ARG SECURITY_SCANS=true
  BUILD --platform=linux/arm64 +image --MODEL=rpi64
  IF [ "$SECURITY_SCANS" = "true" ]
      BUILD --platform=linux/arm64 +image-sbom --MODEL=rpi64
      BUILD --platform=linux/arm64 +trivy-scan --MODEL=rpi64
      BUILD --platform=linux/arm64 +grype-scan --MODEL=rpi64
  END
  
  IF [[ "$FLAVOR" = "ubuntu-20-lts-arm-nvidia-jetson-agx-orin" ]]
    BUILD +prepare-arm-image --MODEL=rpi64 --FLAVOR=${FLAVOR}

  ELSE
    BUILD +arm-image --MODEL=rpi64
  END

arm-container-image:
  ARG MODEL
  BUILD --platform=linux/arm64 +image --MODEL=$MODEL

all-arm-generic:
  BUILD --platform=linux/arm64 +image --MODEL=generic
  BUILD --platform=linux/arm64 +iso --MODEL=generic

go-deps-test:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    # Enable backports repo for debian for swtpm
    RUN . /etc/os-release && echo "deb http://deb.debian.org/debian $VERSION_CODENAME-backports main contrib non-free" > /etc/apt/sources.list.d/backports.list
    WORKDIR /build
    COPY tests/go.mod tests/go.sum ./
    RUN go mod download
    SAVE ARTIFACT go.mod go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum go.sum AS LOCAL go.sum

OSRELEASE:
    COMMAND
    ARG OS_ID
    ARG OS_NAME
    ARG OS_REPO
    ARG OS_VERSION
    ARG OS_LABEL
    ARG VARIANT
    ARG FLAVOR
    ARG GITHUB_REPO
    ARG BUG_REPORT_URL
    ARG HOME_URL

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

    RUN --no-cache echo $(git describe --always --tags --dirty) > VERSION

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
    RUN yamllint .github/workflows/ overlay/

lint:
    BUILD +hadolint
    BUILD +renovate-validate
    BUILD +shellcheck-lint
    BUILD +yamllint

syft:
    FROM anchore/syft:latest
    SAVE ARTIFACT /syft syft

image-sbom:
    # Use base-image so it can read original os-release file
    FROM +base-image
    WORKDIR /build
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG FLAVOR
    ARG VARIANT
    COPY +syft/syft /usr/bin/syft
    RUN syft / -o json=sbom.syft.json -o spdx-json=sbom.spdx.json
    SAVE ARTIFACT /build/sbom.syft.json sbom.syft.json AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-sbom.syft.json
    SAVE ARTIFACT /build/sbom.spdx.json sbom.spdx.json AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-sbom.spdx.json

luet:
    FROM quay.io/luet/base:$LUET_VERSION
    SAVE ARTIFACT /usr/bin/luet /luet

###
### Image Build targets
###

# This generates the framework base by installing luet packages generated with the profile-build + framework-profile.yaml
# file
# Installs everything under the /framework dir and saves that as an artifact
framework-luet:
    FROM golang:alpine
    ARG FLAVOR
    WORKDIR /build
    COPY ./profile-build /build
    COPY framework-profile.yaml /build
    COPY +luet/luet /usr/bin/luet
    RUN go run main.go ${FLAVOR} framework-profile.yaml /framework
    RUN luet cleanup --system-target /framework
    # COPY luet into the final framework
    # TODO: Understand why?
    COPY +luet/luet /framework/usr/bin/luet
    # more cleanup
    RUN rm -rf /framework/var/luet
    RUN rm -rf /framework/var/cache

    SAVE ARTIFACT --keep-own /framework framework-luet

framework:
    FROM alpine
    ARG FLAVOR
    ARG MODEL
    # This ARG does nothing?
    ARG VERSION
    COPY +framework-luet/framework-luet /framework

    # Copy overlay files
    # TODO: Make this also a package?
    COPY overlay/files /framework

    # Copy common overlay files for Raspberry Pi
    IF [ "$MODEL" = "rpi64" ]
        COPY overlay/files-rpi/ /framework
    END

    # Copy flavor-specific overlay files
    IF [[ "$FLAVOR" =~ ^alpine* ]]
        COPY overlay/files-alpine/ /framework
    ELSE IF [ "$FLAVOR" = "fedora" ] || [ "$FLAVOR" = "rockylinux" ]
        COPY overlay/files-fedora/ /framework
    ELSE IF [ "$FLAVOR" = "debian" ] || [ "$FLAVOR" = "ubuntu" ] || [ "$FLAVOR" = "ubuntu-20-lts" ] || [ "$FLAVOR" = "ubuntu-22-lts" ] || [[ "$FLAVOR" =~ ^ubuntu-.*-lts-arm-.*$ ]]
        COPY overlay/files-ubuntu/ /framework
    END

    IF [[ "$FLAVOR" = "ubuntu-20-lts-arm-nvidia-jetson-agx-orin" ]]
        COPY overlay/files-nvidia/ /framework
    END

    SAVE ARTIFACT --keep-own /framework/ framework

build-framework-image:
   COPY +version/VERSION ./
   ARG VERSION=$(cat VERSION)
   ARG FLAVOR
   BUILD +framework-image --VERSION=$VERSION --FLAVOR=$FLAVOR

framework-image:
    FROM scratch
    ARG VERSION
    ARG IMG
    ARG FLAVOR
    COPY (+framework/framework --VERSION=$VERSION --FLAVOR=$FLAVOR) /
    SAVE IMAGE --push $IMAGE_REPOSITORY_ORG/framework:${VERSION}_${FLAVOR}

base-image:
    ARG MODEL
    ARG FLAVOR
    ARG VARIANT
    ARG BUILD_INITRD="true"
    IF [ "$BASE_IMAGE" = "" ]
        # Source the flavor-provided docker file
        FROM DOCKERFILE --build-arg MODEL=$MODEL -f images/Dockerfile.$FLAVOR .
    ELSE 
        FROM $BASE_IMAGE
    END

    ARG KAIROS_VERSION
    IF [ "$KAIROS_VERSION" = "" ]
        COPY +version/VERSION ./
        ARG VERSION=$(cat VERSION)
        RUN echo "version ${VERSION}"
        ARG OS_VERSION=${VERSION}
        RUN rm VERSION
    ELSE 
        ARG OS_VERSION=${KAIROS_VERSION}
    END

    # Includes overlay/files
    COPY (+framework/framework --FLAVOR=$FLAVOR --VERSION=$OS_VERSION --MODEL=$MODEL) /
    # Avoid to accidentally push keys generated by package managers
    RUN rm -rf /etc/ssh/ssh_host_*

    # Enable services
    IF [ -f /sbin/openrc ]
     # Fully remove machine-id, it will be generated
     RUN rm -rf /etc/machine-id
     RUN mkdir -p /etc/runlevels/default && \
      ln -sf /etc/init.d/cos-setup-boot /etc/runlevels/default/cos-setup-boot  && \
      ln -sf /etc/init.d/cos-setup-network /etc/runlevels/default/cos-setup-network  && \
      ln -sf /etc/init.d/cos-setup-reconcile /etc/runlevels/default/cos-setup-reconcile && \
      ln -sf /etc/init.d/kairos-agent /etc/runlevels/default/kairos-agent
    # Otherwise we assume systemd
    ELSE
      # Empty machine-id so we dont accidentally run systemd-firstboot ¬_¬
      RUN rm -rf /etc/machine-id && touch /etc/machine-id && chmod 444 /etc/machine-id
      RUN ls -liah /etc/systemd/system
      RUN systemctl enable cos-setup-reconcile.timer && \
          systemctl enable cos-setup-fs.service && \
          systemctl enable cos-setup-boot.service && \
          systemctl enable cos-setup-network.service
    END

    # TEST KAIROS-AGENT FROM BRANCH
    ARG KAIROS_AGENT_DEV
    ARG KAIROS_AGENT_DEV_BRANCH=main
    IF [ "$KAIROS_AGENT_DEV" = "true" ]
        RUN rm -rf /usr/bin/kairos-agent
        COPY github.com/kairos-io/kairos-agent:$KAIROS_AGENT_DEV_BRANCH+build-kairos-agent/kairos-agent /usr/bin/kairos-agent
    END
    # END

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
    # END

    # TEST KCRYPT FROM BRANCH
    ARG KCRYPT_DEV
    ARG KCRYPT_DEV_BRANCH=main
    IF [ "$KCRYPT_DEV" = "true" ]
        RUN rm /usr/bin/kcrypt
        COPY github.com/kairos-io/kcrypt:$KCRYPT_DEV_BRANCH+build-kcrypt/kcrypt /usr/bin/kcrypt
    END

    # END

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


      IF [ -e "/usr/bin/dracut" ]
          # Regenerate initrd if necessary
          RUN --no-cache kernel=$(ls /lib/modules | head -n1) && depmod -a "${kernel}"
          RUN --no-cache kernel=$(ls /lib/modules | head -n1) && dracut -f "/boot/initrd-${kernel}" "${kernel}" && ln -sf "initrd-${kernel}" /boot/initrd
      END
    END

    # Set /boot/vmlinuz pointing to our kernel so kairos-agent can use it
    # https://github.com/kairos-io/kairos-agent/blob/0288fb111bc568a1bfca59cb09f39302220475b6/pkg/elemental/elemental.go#L548   q
    IF [ "$FLAVOR" = "fedora" ] || [ "$FLAVOR" = "rockylinux" ]
        RUN rm -rf /boot/initramfs-*
    END

    IF [ ! -e "/boot/vmlinuz" ]
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

image:
    ARG BUILD_INITRD="true"
    FROM +base-image --BUILD_INITRD=$BUILD_INITRD
    ARG FLAVOR
    ARG VARIANT
    ARG MODEL
    ARG KAIROS_VERSION
    IF [ "$KAIROS_VERSION" = "" ]
        COPY +version/VERSION ./
        ARG VERSION=$(cat VERSION)
        RUN echo "version ${VERSION}"
        ARG OS_VERSION=${VERSION}
        RUN rm VERSION
    ELSE 
        ARG OS_VERSION=${KAIROS_VERSION}
    END
    ARG OS_ID
    # should we add the model to the resulting iso?
    ARG OS_NAME=${OS_ID}-${VARIANT}-${FLAVOR}
    ARG OS_REPO=quay.io/kairos/${VARIANT}-${FLAVOR}
    ARG OS_LABEL=latest
    DO +OSRELEASE --HOME_URL=https://github.com/kairos-io/kairos --BUG_REPORT_URL=https://github.com/kairos-io/kairos/issues --GITHUB_REPO=kairos-io/kairos --VARIANT=${VARIANT} --FLAVOR=${FLAVOR} --OS_ID=${OS_ID} --OS_LABEL=${OS_LABEL} --OS_NAME=${OS_NAME} --OS_REPO=${OS_REPO} --OS_VERSION=${OS_VERSION}
    SAVE IMAGE $IMAGE

image-rootfs:
    FROM +image
    SAVE ARTIFACT --keep-own /. rootfs

uki-artifacts:
    FROM +image --BUILD_INITRD=false
    RUN /usr/bin/immucore version
    RUN ln -s /usr/bin/immucore /init
    RUN find . \( -path ./sys -prune -o -path ./run -prune -o -path ./dev -prune -o -path ./tmp -prune -o -path ./proc -prune \) -o -print | cpio -R root:root -H newc -o | gzip -2 > /tmp/initramfs.cpio.gz
    RUN echo "console=tty1 console=ttyS0 net.ifnames=1 rd.immucore.debug rd.immucore.uki selinux=0" > /tmp/Cmdline
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
    RUN dnf install -y binutils systemd-boot mtools efitools sbsigntools shim openssl

uki:
    FROM +uki-tools-image
    WORKDIR build
    COPY +uki-artifacts/Kernel Kernel
    COPY +uki-artifacts/Initrd Initrd
    COPY +uki-artifacts/Osrelease Osrelease
    COPY +uki-artifacts/Uname Uname
    COPY +uki-artifacts/Cmdline Cmdline
    ARG KVERSION=$(cat Uname)
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
    SAVE ARTIFACT Uname Uname
    SAVE ARTIFACT $ISO_NAME.unsigned.efi uki.efi AS LOCAL build/$ISO_NAME.unsigned-$KVERSION.efi


uki-signed:
    FROM +uki-tools-image
    # Platform key
    RUN openssl req -new -x509 -subj "/CN=Kairos PK/" -days 3650 -nodes -newkey rsa:2048 -sha256 -keyout PK.key -out PK.crt
    # CER keys are for FW install
    RUN openssl x509 -in PK.crt -out PK.cer -outform DER
    # Key exchange
    RUN openssl req -new -x509 -subj "/CN=Kairos KEK/" -days 3650 -nodes -newkey rsa:2048 -sha256 -keyout KEK.key -out KEK.crt
    # CER keys are for FW install
    RUN openssl x509 -in KEK.crt -out KEK.cer -outform DER
    # Signature DB
    RUN openssl req -new -x509 -subj "/CN=Kairos DB/" -days 3650 -nodes -newkey rsa:2048 -sha256 -keyout DB.key -out DB.crt
    # CER keys are for FW install
    RUN openssl x509 -in DB.crt -out DB.cer -outform DER
    COPY +uki/uki.efi uki.efi
    COPY +uki/Uname Uname
    ARG KVERSION=$(cat Uname)

    RUN sbsign --key DB.key --cert DB.crt --output uki.signed.efi uki.efi


    SAVE ARTIFACT /boot/efi/EFI/fedora/mmx64.efi MokManager.efi
    SAVE ARTIFACT PK.key PK.key AS LOCAL build/PK.key
    SAVE ARTIFACT PK.crt PK.crt AS LOCAL build/PK.crt
    SAVE ARTIFACT PK.cer PK.cer AS LOCAL build/PK.cer
    SAVE ARTIFACT KEK.key KEK.key AS LOCAL build/KEK.key
    SAVE ARTIFACT KEK.crt KEK.crt AS LOCAL build/KEK.crt
    SAVE ARTIFACT KEK.cer KEK.cer AS LOCAL build/KEK.cer
    SAVE ARTIFACT DB.key DB.key AS LOCAL build/DB.key
    SAVE ARTIFACT DB.crt DB.crt AS LOCAL build/DB.crt
    SAVE ARTIFACT DB.cer DB.cer AS LOCAL  build/DB.cer
    SAVE ARTIFACT uki.signed.efi uki.efi AS LOCAL build/$ISO_NAME.signed-$KVERSION.efi

# This target will prepare a disk.img ready with the uki artifact on it for qemu. Just attach it to qemu and mark you vm to boot from that disk
# here we take advantage of the uefi fallback method, which will load an efi binary in /EFI/BOOT/BOOTX64.efi if there is nothing
# else that it can boot from :D Just make sure to have your disk.img set as boot device in qemu.
prepare-uki-disk-image:
    FROM +uki-tools-image
    ARG SIGNED_EFI=false
    IF [ "$SIGNED_EFI" = "true" ]
        COPY +uki-signed/uki.efi .
        COPY +uki-signed/PK.key .
        COPY +uki-signed/PK.crt .
        COPY +uki-signed/PK.cer .
        COPY +uki-signed/KEK.key .
        COPY +uki-signed/KEK.crt .
        COPY +uki-signed/KEK.cer .
        COPY +uki-signed/DB.key .
        COPY +uki-signed/DB.crt .
        COPY +uki-signed/DB.cer .
        COPY +uki-signed/MokManager.efi .
    ELSE
        COPY +uki/uki.efi .
    END
    RUN dd if=/dev/zero of=disk.img bs=1G count=1
    RUN mformat -i disk.img -F  ::
    RUN mmd -i disk.img ::/EFI
    RUN mmd -i disk.img ::/EFI/BOOT
    RUN mcopy -i disk.img uki.efi ::/EFI/BOOT/BOOTX64.efi
    IF [ "$SIGNED_EFI" = "true" ]
        RUN mcopy -i disk.img PK.key ::/EFI/BOOT/PK.key
        RUN mcopy -i disk.img PK.crt ::/EFI/BOOT/PK.crt
        RUN mcopy -i disk.img PK.cer ::/EFI/BOOT/PK.cer
        RUN mcopy -i disk.img KEK.key ::/EFI/BOOT/KEK.key
        RUN mcopy -i disk.img KEK.crt ::/EFI/BOOT/KEK.crt
        RUN mcopy -i disk.img KEK.cer ::/EFI/BOOT/KEK.cer
        RUN mcopy -i disk.img DB.key ::/EFI/BOOT/DB.key
        RUN mcopy -i disk.img DB.crt ::/EFI/BOOT/DB.crt
        RUN mcopy -i disk.img DB.cer ::/EFI/BOOT/DB.cer
        RUN mcopy -i disk.img MokManager.efi ::/EFI/BOOT/mmx64.efi
    END
    RUN mdir -i disk.img ::/EFI/BOOT
    SAVE ARTIFACT disk.img AS LOCAL build/disk.img


###
### Artifacts targets (ISO, netboot, ARM)
###

iso:
    ARG OSBUILDER_IMAGE
    ARG ISO_NAME=${OS_ID}
    ARG IMG=docker:$IMAGE
    ARG overlay=overlay/files-iso
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY . ./
    COPY --keep-own +image-rootfs/rootfs /build/image
    RUN /entrypoint.sh --name $ISO_NAME --debug build-iso --squash-no-compression --date=false dir:/build/image --overlay-iso /build/${overlay} --output /build/
    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.iso
    SAVE ARTIFACT /build/$ISO_NAME.iso.sha256 kairos.iso.sha256 AS LOCAL build/$ISO_NAME.iso.sha256

# This target builds an iso using a remote docker image as rootfs instead of building the whole rootfs
# This should be really fast as it uses an existing image. This requires a pushed image from the +image target
# defaults to use the $IMAGE name (so ttl.sh/core-opensuse-leap:latest)
# you can override either the full thing by setting --IMG=docker:REPO/IMAGE:TAG
# or by --IMAGE=REPO/IMAGE:TAG
iso-remote:
    ARG OSBUILDER_IMAGE
    ARG ISO_NAME=${OS_ID}
    ARG IMG=docker:$IMAGE
    ARG overlay=overlay/files-iso
    FROM $OSBUILDER_IMAGE
    WORKDIR /build
    COPY . ./
    RUN /entrypoint.sh --name $ISO_NAME --debug build-iso --squash-no-compression --date=false $IMG --overlay-iso /build/${overlay} --output /build/
    SAVE ARTIFACT /build/$ISO_NAME.iso kairos.iso AS LOCAL build/$ISO_NAME.iso
    SAVE ARTIFACT /build/$ISO_NAME.iso.sha256 kairos.iso.sha256 AS LOCAL build/$ISO_NAME.iso.sha256

netboot:
   ARG OSBUILDER_IMAGE
   FROM $OSBUILDER_IMAGE
   COPY +version/VERSION ./
   ARG VERSION=$(cat VERSION)
   RUN echo "version ${VERSION}"
   ARG ISO_NAME=${OS_ID}
   ARG FROM_ARTIFACT
   WORKDIR /build
   ARG RELEASE_URL

   COPY . .
   IF [ "$FROM_ARTIFACT" = "" ]
        COPY +iso/kairos.iso kairos.iso
        RUN /build/scripts/netboot.sh kairos.iso $ISO_NAME $VERSION
   ELSE
        RUN /build/scripts/netboot.sh $FROM_ARTIFACT $ISO_NAME $VERSION
   END

   SAVE ARTIFACT /build/$ISO_NAME.squashfs squashfs AS LOCAL build/$ISO_NAME.squashfs
   SAVE ARTIFACT /build/$ISO_NAME-kernel kernel AS LOCAL build/$ISO_NAME-kernel
   SAVE ARTIFACT /build/$ISO_NAME-initrd initrd AS LOCAL build/$ISO_NAME-initrd
   SAVE ARTIFACT /build/$ISO_NAME.ipxe ipxe AS LOCAL build/$ISO_NAME.ipxe

arm-image:
  ARG OSBUILDER_IMAGE
  ARG COMPRESS_IMG=true
  FROM $OSBUILDER_IMAGE
  ARG MODEL=rpi64
  ARG IMAGE_NAME=${FLAVOR}.img
  WORKDIR /build
  # These sizes are in MB
  ENV SIZE="15200"
  IF [[ "$FLAVOR" =~ ^ubuntu* ]]
    ENV STATE_SIZE="6900"
    ENV RECOVERY_SIZE="4600"
    ENV DEFAULT_ACTIVE_SIZE="2500"
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
      RUN xz -v /build/$IMAGE_NAME
      SAVE ARTIFACT /build/$IMAGE_NAME.xz img AS LOCAL build/$IMAGE_NAME.xz
  ELSE
      SAVE ARTIFACT /build/$IMAGE_NAME img AS LOCAL build/$IMAGE_NAME
  END
  SAVE ARTIFACT /build/$IMAGE_NAME.sha256 img-sha256 AS LOCAL build/$IMAGE_NAME.sha256

prepare-arm-image:
  ARG OSBUILDER_IMAGE
  ARG COMPRESS_IMG=true
  FROM $OSBUILDER_IMAGE
  ARG MODEL=rpi64
  ARG IMAGE_NAME=${FLAVOR}.img
  WORKDIR /build
  # These sizes are in MB
  ENV SIZE="15200"
  IF [[ "$FLAVOR" =~ ^ubuntu* ]]
    ENV STATE_SIZE="6900"
    ENV RECOVERY_SIZE="4600"
    ENV DEFAULT_ACTIVE_SIZE="2500"
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
    FROM ubuntu
    ARG ipxe_script
    RUN apt update
    RUN apt install -y -o Acquire::Retries=50 \
                           mtools syslinux isolinux gcc-arm-none-eabi git make gcc liblzma-dev mkisofs xorriso
                           # jq docker
    WORKDIR /build
    ARG ISO_NAME=${OS_ID}        
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
    SAVE ARTIFACT /build/ipxe/src/bin/ipxe.iso iso AS LOCAL build/${ISO_NAME}-ipxe.iso.ipxe
    SAVE ARTIFACT /build/ipxe/src/bin/ipxe.usb usb AS LOCAL build/${ISO_NAME}-ipxe-usb.img.ipxe

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
    # Use base-image so it can read original os-release file
    FROM +base-image
    COPY +trivy/trivy /trivy
    COPY +trivy/contrib /contrib
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG FLAVOR
    ARG VARIANT
    WORKDIR /build
    RUN /trivy filesystem --skip-dirs /tmp --timeout 30m --format sarif -o report.sarif --no-progress /
    RUN /trivy filesystem --skip-dirs /tmp --timeout 30m --format template --template "@/contrib/html.tpl" -o report.html --no-progress /
    RUN /trivy filesystem --skip-dirs /tmp --timeout 30m -f json -o results.json --no-progress /
    SAVE ARTIFACT /build/report.sarif report.sarif AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-trivy.sarif
    SAVE ARTIFACT /build/report.html report.html AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-trivy.html
    SAVE ARTIFACT /build/results.json results.json AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-trivy.json

grype:
    FROM anchore/grype
    SAVE ARTIFACT /grype /grype

grype-scan:
    # Use base-image so it can read original os-release file
    FROM +base-image
    COPY +grype/grype /grype
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)
    ARG FLAVOR
    ARG VARIANT
    WORKDIR /build
    RUN /grype dir:/ --output sarif --add-cpes-if-none --file report.sarif
    RUN /grype dir:/ --output json --add-cpes-if-none --file report.json
    SAVE ARTIFACT /build/report.sarif report.sarif AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-grype.sarif
    SAVE ARTIFACT /build/report.json report.json AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-grype.json


###
### Test targets
###
# usage e.g. ./earthly.sh +run-qemu-datasource-tests --FLAVOR=alpine-opensuse-leap --FROM_ARTIFACTS=true
run-qemu-datasource-tests:
    FROM +go-deps-test
    RUN apt update
    RUN apt install -y qemu-system-x86 qemu-utils golang git swtpm
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
    FROM +go-deps-test
    COPY . /test
    WORKDIR /test

    ARG ISO_NAME=${OS_ID}
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)

    RUN apt update
    RUN apt install -y qemu qemu-utils qemu-system git swtpm && apt clean

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
    RUN apt update
    RUN apt install -y qemu-system-x86 qemu-utils git swtpm && apt clean
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
        ENV ISO=/build/$PREBUILT_ISO
    ELSE
        COPY +iso/kairos.iso kairos.iso
        ENV ISO=/build/kairos.iso
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
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:8h

    COPY +luet/luet /usr/bin/luet
    RUN luet util unpack $BUNDLE_IMAGE build
    SAVE ARTIFACT build AS LOCAL build

## Push build artifacts as BUNDLE_IMAGE (expected arg, common is to use ttl.sh/$(uuidgen):8h)
push-build-artifacts:
    ARG OSBUILDER_IMAGE
    FROM $OSBUILDER_IMAGE
    RUN zypper in -y jq docker
    COPY +uuidgen/UUIDGEN ./
    COPY +version/VERSION ./
    ARG UUIDGEN=$(cat UUIDGEN)
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:8h

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
    ARG BUNDLE_IMAGE=ttl.sh/$UUIDGEN:8h
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

    FROM +image
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
