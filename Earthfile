VERSION 0.6
FROM alpine
ARG FLAVOR=opensuse
ARG IMAGE=quay.io/c3os/c3os:${FLAVOR}-latest
ARG LUET_VERSION=0.32.4
ARG REPOSITORIES_FILE=repositories.yaml

IF [ "$FLAVOR" = "fedora" ] || [ "$FLAVOR" = "tumbleweed" ] || [ "$FLAVOR" = "ubuntu" ]
    ARG REPOSITORIES_FILE=repositories.yaml.${FLAVOR}
END

ARG COSIGN_SKIP=".*quay.io/c3os/.*"

# TODO: This should match for each flavor
ARG COSIGN_REPOSITORY=raccos/releases-teal
ARG COSIGN_EXPERIMENTAL=0
ARG CGO_ENABLED=0
ARG ELEMENTAL_IMAGE=quay.io/costoolkit/elemental:v0.0.15-8a78e6b


all:
  BUILD +docker
  BUILD +iso
  BUILD +netboot
  BUILD +ipxe-iso

all-arm:
  BUILD --platform=linux/arm64 +docker
  BUILD +arm-image

go-deps:
    FROM golang
    WORKDIR /build
    COPY go.mod go.sum ./
    RUN go mod download
    RUN apt-get update && apt-get install -y upx
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum AS LOCAL go.sum

test:
    FROM +go-deps
    WORKDIR /build
    RUN go get github.com/onsi/gomega/...
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/internal@v2.1.4
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/generators@v2.1.4
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/labels@v2.1.4
    RUN go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo
    RUN curl https://luet.io/install.sh | sh
    COPY . .
    RUN ginkgo run --fail-fast --slow-spec-threshold 30s --covermode=atomic --coverprofile=coverage.out -p -r ./pkg ./internal ./cmd
    SAVE ARTIFACT coverage.out AS LOCAL coverage.out

BUILD_GOLANG:
    COMMAND
    WORKDIR /build
    COPY . ./
    ARG CGO_ENABLED
    ARG BIN
    ARG SRC
    ENV CGO_ENABLED=${CGO_ENABLED}

    RUN go build -ldflags "-s -w" -o ${BIN} ./cmd/${SRC} && upx ${BIN}
    SAVE ARTIFACT ${BIN} ${BIN} AS LOCAL build/${BIN}

build-c3os-cli:
    FROM +go-deps
    DO +BUILD_GOLANG --BIN=c3os --SRC=cli --CGO_ENABLED=$CGO_ENABLED

build-c3os-agent:
    FROM +go-deps
    DO +BUILD_GOLANG --BIN=c3os-agent --SRC=agent --CGO_ENABLED=$CGO_ENABLED

build-c3os-agent-provider:
    FROM +go-deps
    DO +BUILD_GOLANG --BIN=agent-provider-c3os --SRC=provider --CGO_ENABLED=$CGO_ENABLED

build:
    BUILD +build-c3os-cli
    BUILD +build-c3os-agent
    BUILD +build-c3os-agent-provider

luet:
    FROM quay.io/luet/base:$LUET_VERSION
    SAVE ARTIFACT /usr/bin/luet /luet

framework:
    ARG COSIGN_SKIP
    ARG REPOSITORIES_FILE
    ARG COSIGN_EXPERIMENTAL
    ARG COSIGN_REPOSITORY
    ARG WITH_KERNEL

    FROM alpine
    COPY +luet/luet /usr/bin/luet

    # cosign keyless verify
    ENV COSIGN_EXPERIMENTAL=${COSIGN_EXPERIMENTAL}
    # Repo containing signatures
    ENV COSIGN_REPOSITORY=${COSIGN_REPOSITORY}
    # Skip this repo artifacts verify as they are not signed
    ENV COSIGN_SKIP=${COSIGN_SKIP}

    # Copy the luet config file pointing to the upgrade repository
    COPY repositories/$REPOSITORIES_FILE /etc/luet/luet.yaml

    ENV USER=root

    IF [ "$WITH_KERNEL" = "true" ] || [ "$FLAVOR" = "alpine" ] || [ "$FLAVOR" = "fedora" ] || [ "$FLAVOR" = "ubuntu" ] || [ "$FLAVOR" = "alpine-arm-rpi" ]
        RUN /usr/bin/luet install -y --system-target /framework \
            meta/cos-verify \
            meta/cos-core \
            cloud-config/recovery \
            cloud-config/live \
            cloud-config/network \
            cloud-config/boot-assessment \
            cloud-config/rootfs \
            utils/edgevpn \
            utils/k9s \
            system-openrc/cos-setup \
            utils/nerdctl \
            system/kernel \
            system/dracut-initrd
    ELSE
        RUN /usr/bin/luet install -y --system-target /framework \ 
            meta/cos-verify \
            meta/cos-core \ 
            utils/edgevpn \
            cloud-config/recovery \
            cloud-config/live \
            cloud-config/boot-assessment \
            cloud-config/network \
            cloud-config/rootfs \
            systemd-service/edgevpn \
            utils/k9s \
            container/kubectl \
            utils/nerdctl
    END

    RUN /usr/bin/luet cleanup --system-target /framework
    COPY overlay/files /framework
    RUN rm -rf /framework/var/luet
    RUN rm -rf /framework/var/cache
    SAVE ARTIFACT /framework/ framework

framework-image:
    FROM scratch
    ARG IMG
    COPY +framework/framework /
    SAVE IMAGE $IMG

framework-images:
    ARG IMG
    BUILD +framework-image --WITH_KERNEL=true
    BUILD +framework-image --WITH_KERNEL=false --IMG=$IMG-kernel

docker:
    ARG K3S_VERSION
    IF [ "$BASE_IMAGE" = "" ]
        # Source the flavor-provided docker file
        FROM DOCKERFILE -f images/Dockerfile.$FLAVOR .
    ELSE 
        FROM $BASE_IMAGE
    END
    ARG C3OS_VERSION
    IF [ "$K3S_VERSION" = "" ]
        ARG OS_VERSION=c3OS${C3OS_VERSION}
    ELSE
        ARG OS_VERSION=${K3S_VERSION}+k3s1-c3OS${C3OS_VERSION}
    END
    ARG OS_ID=c3os
    ARG FLAVOR
    ARG OS_NAME=${OS_ID}-${FLAVOR}
    ARG OS_REPO=quay.io/c3os/c3os
    ARG OS_LABEL=${FLAVOR}-latest
    ARG WITH_PROVIDER=true
    ARG WITH_CLI=true
    ENV OS_LABEL=$OS_LABEL
    ENV OS_NAME=$OS_NAME
    ENV OS_ID=$OS_ID
    ENV OS_VERSION=$OS_VERSION
    ENV OS_REPO=$OS_REPO

    # Includes overlay/files
    COPY +framework/framework /

    # Copy flavor-specific overlay files
    IF [ "$FLAVOR" = "alpine" ]
        COPY overlay/files-alpine/ /
    ELSE IF [ "$FLAVOR" = "alpine-arm-rpi" ]
        COPY overlay/files-alpine/ /
        COPY overlay/files-opensuse-arm-rpi/ /
    ELSE IF [ "$FLAVOR" = "opensuse-arm-rpi" ]
        COPY overlay/files-opensuse-arm-rpi/ /
    END

    # Copy c3os binaries
    COPY +build-c3os-agent/c3os-agent /usr/bin/c3os-agent
    IF [ "$WITH_CLI" = "true" ]
        COPY +build-c3os-cli/c3os /usr/bin/c3os
    END
    IF [ "$WITH_PROVIDER" = "true" ]
        COPY +build-c3os-agent-provider/agent-provider-c3os /usr/bin/agent-provider-c3os
    END

    # update OS-release file
    RUN envsubst >/etc/os-release </usr/lib/os-release.tmpl && \
        rm /usr/lib/os-release.tmpl

    # Regenerate initrd if necessary
    IF [ "$FLAVOR" = "opensuse" ] || [ "$FLAVOR" = "opensuse-arm-rpi" ] || [ "$FLAVOR" = "tumbleweed-arm-rpi" ]
     RUN mkinitrd
    ELSE IF [ "$FLAVOR" = "ubuntu" ]
     RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && \
            ln -sf "${kernel#/boot/}" /boot/vmlinuz
     RUN kernel=$(ls /lib/modules | head -n1) && \
            dracut -f "/boot/initrd-${kernel}" "${kernel}" && \
            ln -sf "initrd-${kernel}" /boot/initrd
     RUN kernel=$(ls /lib/modules | head -n1) && depmod -a "${kernel}"
    END

    # If it's an ARM flavor, we want a symlink here
    IF [ "$FLAVOR" = "alpine-arm-rpi" ] || [ "$FLAVOR" = "opensuse-arm-rpi" ] || [ "$FLAVOR" = "tumbleweed-arm-rpi" ]
     RUN ln -sf Image /boot/vmlinuz
    END

    SAVE IMAGE $IMAGE

docker-rootfs:
    FROM +docker
    SAVE ARTIFACT /. rootfs

elemental:
    ARG ELEMENTAL_IMAGE
    FROM ${ELEMENTAL_IMAGE}
    SAVE ARTIFACT /usr/bin/elemental elemental

iso:
    ARG ELEMENTAL_IMAGE
    ARG ISO_NAME=${OS_ID}
    ARG IMG=docker:$IMAGE
    ARG overlay=overlay/files-iso
    ARG TOOLKIT_REPOSITORY=quay.io/costoolkit/releases-teal
    FROM $ELEMENTAL_IMAGE
    RUN zypper in -y jq docker
    WORKDIR /build
    COPY . ./
    WITH DOCKER --allow-privileged --load $IMAGE=(+docker)
        RUN elemental --repo $TOOLKIT_REPOSITORY --name $ISO_NAME --debug build-iso --date=false --local --overlay-iso /build/${overlay} $IMAGE --output /build/
    END
    # See: https://github.com/rancher/elemental-cli/issues/228
    RUN sha256sum $ISO_NAME.iso > $ISO_NAME.iso.sha256
    SAVE ARTIFACT /build/$ISO_NAME.iso c3os.iso AS LOCAL build/$ISO_NAME.iso
    SAVE ARTIFACT /build/$ISO_NAME.iso.sha256 c3os.iso.sha256 AS LOCAL build/$ISO_NAME.iso.sha256

netboot:
   FROM opensuse/leap
   ARG ISO_NAME=${OS_ID}
   WORKDIR /build
   COPY +iso/c3os.iso c3os.iso
   COPY . .
   RUN zypper in -y cdrtools
   RUN /build/scripts/netboot.sh c3os.iso $ISO_NAME
   SAVE ARTIFACT /build/$ISO_NAME.squashfs squashfs AS LOCAL build/$ISO_NAME.squashfs
   SAVE ARTIFACT /build/$ISO_NAME-kernel kernel AS LOCAL build/$ISO_NAME-kernel
   SAVE ARTIFACT /build/$ISO_NAME-initrd initrd AS LOCAL build/$ISO_NAME-initrd
   SAVE ARTIFACT /build/$ISO_NAME.ipxe ipxe AS LOCAL build/$ISO_NAME.ipxe

arm-image:
  ARG ELEMENTAL_IMAGE
  FROM $ELEMENTAL_IMAGE
  ARG MODEL=rpi64
  ARG IMAGE_NAME=${FLAVOR}.img
  RUN zypper in -y jq docker git curl gptfdisk kpartx
  #COPY +luet/luet /usr/bin/luet
  WORKDIR /build
  RUN git clone https://github.com/rancher/elemental-toolkit && mkdir elemental-toolkit/build
  RUN curl https://luet.io/install.sh | sh
  ENV STATE_SIZE="6200"
  ENV RECOVERY_SIZE="4200"
  ENV SIZE="15200"
  ENV DEFAULT_ACTIVE_SIZE="2000"
  COPY --platform=linux/arm64 +docker-rootfs/rootfs /build/image
  # With docker is required for loop devices
  WITH DOCKER --allow-privileged
    RUN cd elemental-toolkit && \
          ./images/arm-img-builder.sh --model $MODEL --directory "/build/image" build/$IMAGE_NAME && mv build ../
  END
  RUN xz -v /build/build/$IMAGE_NAME
  SAVE ARTIFACT /build/build/$IMAGE_NAME.xz img AS LOCAL build/$IMAGE_NAME
  SAVE ARTIFACT /build/build/$IMAGE_NAME.sha256 img-sha256 AS LOCAL build/$IMAGE_NAME.sha256

ipxe-iso:
    FROM ubuntu
    ARG ipxe_script
    RUN apt update
    RUN apt install -y -o Acquire::Retries=50 \
                           mtools syslinux isolinux gcc-arm-none-eabi git make gcc liblzma-dev mkisofs xorriso
                           # jq docker
    WORKDIR /build
    ARG ISO_NAME=${OS_ID}
    RUN git clone https://github.com/ipxe/ipxe
    IF [ "$ipxe_script" = "" ]
        COPY +netboot/ipxe /build/ipxe/script.ipxe
    ELSE
        COPY $ipxe_script /build/ipxe/script.ipxe
    END
    RUN cd ipxe/src && make EMBED=/build/ipxe/script.ipxe
    SAVE ARTIFACT /build/ipxe/src/bin/ipxe.iso iso AS LOCAL build/${ISO_NAME}-ipxe.iso.ipxe
    SAVE ARTIFACT /build/ipxe/src/bin/ipxe.usb usb AS LOCAL build/${ISO_NAME}-ipxe-usb.img.ipxe


## Security targets
trivy:
    FROM aquasec/trivy
    SAVE ARTIFACT /usr/local/bin/trivy /trivy

trivy-scan:
    ARG SEVERITY=CRITICAL
    FROM +docker
    COPY +trivy/trivy /trivy
    RUN /trivy filesystem --severity $SEVERITY --exit-code 1 --no-progress /

linux-bench:
    FROM golang
    GIT CLONE https://github.com/aquasecurity/linux-bench /linux-bench-src
    RUN cd /linux-bench-src && CGO_ENABLED=0 go build -o linux-bench . && mv linux-bench /
    SAVE ARTIFACT /linux-bench /linux-bench

# The target below should run on a live host instead. 
# However, some checks are relevant as well at container level.
# It is good enough for a quick assessment.
linux-bench-scan:
    FROM +docker
    GIT CLONE https://github.com/aquasecurity/linux-bench /build/linux-bench
    WORKDIR /build/linux-bench
    COPY +linux-bench/linux-bench /build/linux-bench/linux-bench
    RUN /build/linux-bench/linux-bench