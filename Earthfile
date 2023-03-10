VERSION 0.6
FROM alpine
ARG VARIANT=core # core, lite, framework
ARG FLAVOR=opensuse-leap
ARG IMAGE=quay.io/kairos/${VARIANT}-${FLAVOR}:latest
ARG ISO_NAME=kairos-${VARIANT}-${FLAVOR}
# renovate: datasource=docker depName=quay.io/luet/base
ARG LUET_VERSION=0.34.0
ARG OS_ID=kairos
ARG REPOSITORIES_FILE=framework-profile.yaml
# renovate: datasource=docker depName=aquasec/trivy
ARG TRIVY_VERSION=0.37.3
ARG COSIGN_SKIP=".*quay.io/kairos/.*"

IF [ "$FLAVOR" = "ubuntu" ]
    ARG COSIGN_REPOSITORY=raccos/releases-orange
ELSE
    ARG COSIGN_REPOSITORY=raccos/releases-teal
END
ARG COSIGN_EXPERIMENTAL=0
ARG CGO_ENABLED=0
# renovate: datasource=docker depName=quay.io/kairos/osbuilder-tools versioning=semver-coerced
ARG OSBUILDER_VERSION=v0.5.2
ARG OSBUILDER_IMAGE=quay.io/kairos/osbuilder-tools:$OSBUILDER_VERSION
ARG GOLINT_VERSION=1.47.3
# renovate: datasource=docker depName=golang
ARG GO_VERSION=1.18
# renovate: datasource=docker depName=hadolint/hadolint versioning=docker
ARG HADOLINT_VERSION=2.12.0-alpine
# renovate: datasource=docker depName=renovate/renovate versioning=docker
ARG RENOVATE_VERSION=34
# renovate: datasource=docker depName=koalaman/shellcheck-alpine versioning=docker
ARG SHELLCHECK_VERSION=v0.9.0

ARG IMAGE_REPOSITORY_ORG=quay.io/kairos


all:
  BUILD +image
  BUILD +image-sbom
  BUILD +trivy-scan
  BUILD +grype-scan
  BUILD +iso
  BUILD +netboot
  BUILD +ipxe-iso

all-arm:
  BUILD --platform=linux/arm64 +image
  BUILD +image-sbom
  BUILD +trivy-scan
  BUILD +grype-scan
  BUILD +arm-image

go-deps:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    WORKDIR /build
    COPY go.mod go.sum ./
    COPY sdk sdk
    RUN go mod download
    RUN apt-get update && apt-get install -y upx
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT go.sum AS LOCAL go.sum


ginkgo:
    FROM +go-deps
    WORKDIR /build
    RUN go get github.com/onsi/gomega/...
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/internal@v2.1.4
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/generators@v2.1.4
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/labels@v2.1.4
    RUN go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo

test:
    FROM +ginkgo
    WORKDIR /build
    RUN go get github.com/onsi/gomega/...
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/internal@v2.1.4
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/generators@v2.1.4
    RUN go get github.com/onsi/ginkgo/v2/ginkgo/labels@v2.1.4
    RUN go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo
    COPY +luet/luet /usr/bin/luet
    COPY . .
    ENV ACK_GINKGO_DEPRECATIONS=2.5.1
    RUN ginkgo run --fail-fast --slow-spec-threshold 30s --covermode=atomic --coverprofile=coverage.out -p -r ./pkg ./internal ./cmd ./sdk
    SAVE ARTIFACT coverage.out AS LOCAL coverage.out

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
    RUN envsubst >/etc/os-release </usr/lib/os-release.tmpl

BUILD_GOLANG:
    COMMAND
    WORKDIR /build
    COPY . ./
    ARG CGO_ENABLED
    ARG BIN
    ARG SRC

    ENV CGO_ENABLED=${CGO_ENABLED}
    ARG LDFLAGS="-s -w -X 'github.com/kairos-io/kairos/internal/common.VERSION=$VERSION'"
    RUN echo "Building ${BIN} from ${SRC} using ${VERSION}"
    RUN echo ${LDFLAGS}
    RUN go build -o ${BIN} -ldflags "${LDFLAGS}" ./cmd/${SRC} && upx ${BIN}
    SAVE ARTIFACT ${BIN} ${BIN} AS LOCAL build/${BIN}

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


build-kairos-agent:
    FROM +go-deps
    COPY +webui-deps/node_modules ./internal/webui/public/node_modules
    COPY +docs/public/local ./internal/webui/public/local
    DO +BUILD_GOLANG --BIN=kairos-agent --SRC=agent --CGO_ENABLED=$CGO_ENABLED

build:
    BUILD +build-kairos-agent

dist:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    COPY +luet/luet /usr/bin/luet
    RUN mkdir -p /etc/luet/repos.conf.d/
    RUN luet repo add kairos --yes --url quay.io/kairos/packages --type docker
    RUN luet install -y utils/goreleaser
    WORKDIR /build
    COPY . .
    RUN goreleaser build --rm-dist --skip-validate --snapshot
    SAVE ARTIFACT /build/dist/* AS LOCAL dist/

golint:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    ARG GOLINT_VERSION
    RUN wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v$GOLINT_VERSION
    WORKDIR /build
    COPY . .
    RUN golangci-lint run

hadolint:
    ARG HADOLINT_VERSION
    FROM hadolint/hadolint:$HADOLINT_VERSION
    WORKDIR /images
    COPY images .
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
    BUILD +golint
    BUILD +hadolint
    BUILD +renovate-validate
    BUILD +shellcheck-lint
    BUILD +yamllint

syft:
    FROM anchore/syft:latest
    SAVE ARTIFACT /syft syft

image-sbom:
    FROM +image
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

framework:
    ARG COSIGN_SKIP
    ARG REPOSITORIES_FILE
    ARG COSIGN_EXPERIMENTAL
    ARG COSIGN_REPOSITORY
    ARG FLAVOR
    ARG VERSION
    ARG LDFLAGS="-s -w -X 'github.com/kairos-io/kairos/internal/common.VERSION=$VERSION'"

    FROM golang:alpine
    WORKDIR /build
    COPY +luet/luet /usr/bin/luet

    # cosign keyless verify
    ENV COSIGN_EXPERIMENTAL=${COSIGN_EXPERIMENTAL}
    # Repo containing signatures
    ENV COSIGN_REPOSITORY=${COSIGN_REPOSITORY}
    # Skip this repo artifacts verify as they are not signed
    ENV COSIGN_SKIP=${COSIGN_SKIP}

    ENV USER=root

    COPY . /build

    RUN go run -ldflags "${LDFLAGS}" ./cmd/profile-build/main.go ${FLAVOR} $REPOSITORIES_FILE /framework

    # Copy kairos binaries
    COPY +build-kairos-agent/kairos-agent /framework/usr/bin/kairos-agent
    COPY +luet/luet /framework/usr/bin/luet

    RUN luet cleanup --system-target /framework

    # Copy overlay files
    COPY overlay/files /framework
    # Copy flavor-specific overlay files
    IF [ "$FLAVOR" = "alpine-opensuse-leap" ] || [ "$FLAVOR" = "alpine-ubuntu" ]
        COPY overlay/files-alpine/ /framework
    END
    
    IF [ "$FLAVOR" = "alpine-arm-rpi" ]
        COPY overlay/files-alpine/ /framework
        COPY overlay/files-opensuse-arm-rpi/ /framework
    ELSE IF [ "$FLAVOR" = "opensuse-leap-arm-rpi" ] || [ "$FLAVOR" = "opensuse-tumbleweed-arm-rpi" ]
        COPY overlay/files-opensuse-arm-rpi/ /framework
    ELSE IF [ "$FLAVOR" = "fedora" ] || [ "$FLAVOR" = "rockylinux" ]
        COPY overlay/files-fedora/ /framework
    ELSE IF [ "$FLAVOR" = "debian" ] || [ "$FLAVOR" = "ubuntu" ] || [ "$FLAVOR" = "ubuntu-20-lts" ] || [ "$FLAVOR" = "ubuntu-22-lts" ]
        COPY overlay/files-ubuntu/ /framework
    END

    RUN rm -rf /framework/var/luet
    RUN rm -rf /framework/var/cache
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
    ARG FLAVOR
    ARG VARIANT
    IF [ "$BASE_IMAGE" = "" ]
        # Source the flavor-provided docker file
        FROM DOCKERFILE -f images/Dockerfile.$FLAVOR .
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
    
    ARG OS_ID
    ARG OS_NAME=${OS_ID}-${VARIANT}-${FLAVOR}
    ARG OS_REPO=quay.io/kairos/${VARIANT}-${FLAVOR}
    ARG OS_LABEL=latest

    # Includes overlay/files
    COPY (+framework/framework --FLAVOR=$FLAVOR --VERSION=$OS_VERSION) /

    RUN rm -rf /etc/machine-id && touch /etc/machine-id && chmod 444 /etc/machine-id

    # Avoid to accidentally push keys generated by package managers
    RUN rm -rf /etc/ssh/ssh_host_*

    # Enable services
    IF [ -f /sbin/openrc ]
     RUN mkdir -p /etc/runlevels/default && \
      ln -sf /etc/init.d/cos-setup-boot /etc/runlevels/default/cos-setup-boot  && \
      ln -sf /etc/init.d/cos-setup-network /etc/runlevels/default/cos-setup-network  && \
      ln -sf /etc/init.d/cos-setup-reconcile /etc/runlevels/default/cos-setup-reconcile && \
      ln -sf /etc/init.d/kairos-agent /etc/runlevels/default/kairos-agent
    # Otherwise we assume systemd
    ELSE
      RUN ls -liah /etc/systemd/system
      RUN systemctl enable cos-setup-reconcile.timer && \
          systemctl enable cos-setup-fs.service && \
          systemctl enable cos-setup-boot.service && \
          systemctl enable cos-setup-network.service
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
    # END

    IF [ "$FLAVOR" = "debian" ]
	    RUN rm -rf /boot/initrd.img-*
    END
    # Regenerate initrd if necessary
    IF [ "$FLAVOR" = "opensuse-leap" ] || [ "$FLAVOR" = "opensuse-leap-arm-rpi" ] || [ "$FLAVOR" = "opensuse-tumbleweed-arm-rpi" ] || [ "$FLAVOR" = "opensuse-tumbleweed" ]
     RUN mkinitrd
    ELSE IF [ "$FLAVOR" = "fedora" ] || [ "$FLAVOR" = "rockylinux" ]
     RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && \
            ln -sf "${kernel#/boot/}" /boot/vmlinuz
     RUN kernel=$(ls /lib/modules | head -n1) && \
            dracut -v -N -f "/boot/initrd-${kernel}" "${kernel}" && \
            ln -sf "initrd-${kernel}" /boot/initrd
     RUN kernel=$(ls /lib/modules | head -n1) && depmod -a "${kernel}"
     # https://github.com/kairos-io/elemental-cli/blob/23ca64435fedb9f521c95e798d2c98d2714c53bd/pkg/elemental/elemental.go#L553
     RUN rm -rf /boot/initramfs-*
    ELSE IF [ "$FLAVOR" = "debian" ] || [ "$FLAVOR" = "ubuntu" ] || [ "$FLAVOR" = "ubuntu-20-lts" ] || [ "$FLAVOR" = "ubuntu-22-lts" ]
     RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && \
            ln -sf "${kernel#/boot/}" /boot/vmlinuz
     RUN kernel=$(ls /lib/modules | head -n1) && \
            dracut -f "/boot/initrd-${kernel}" "${kernel}" && \
            ln -sf "initrd-${kernel}" /boot/initrd
     RUN kernel=$(ls /lib/modules | head -n1) && depmod -a "${kernel}"
    END

    IF [ ! -e "/boot/vmlinuz" ]
        # If it's an ARM flavor, we want a symlink here from zImage/Image
        IF [ -e "/boot/Image" ]
            RUN ln -sf Image /boot/vmlinuz
        ELSE IF [ -e "/boot/zImage" ]
            RUN ln -sf zImage /boot/vmlinuz
        ELSE
            RUN kernel=$(ls /lib/modules | head -n1) && \
             ln -sf "${kernel#/boot/}" /boot/vmlinuz
        END
    END

    RUN rm -rf /tmp/*

image:
    FROM +base-image
    DO +OSRELEASE --HOME_URL=https://github.com/kairos-io/kairos --BUG_REPORT_URL=https://github.com/kairos-io/kairos/issues --GITHUB_REPO=kairos-io/kairos --VARIANT=${VARIANT} --FLAVOR=${FLAVOR} --OS_ID=${OS_ID} --OS_LABEL=${OS_LABEL} --OS_NAME=${OS_NAME} --OS_REPO=${OS_REPO} --OS_VERSION=${OS_VERSION}
    SAVE IMAGE $IMAGE

image-rootfs:
    FROM +image
    SAVE ARTIFACT --keep-own /. rootfs

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
  FROM $OSBUILDER_IMAGE
  ARG MODEL=rpi64
  ARG IMAGE_NAME=${FLAVOR}.img
  WORKDIR /build
  ENV STATE_SIZE="6200"
  ENV RECOVERY_SIZE="4200"
  ENV SIZE="15200"
  ENV DEFAULT_ACTIVE_SIZE="2000"
  COPY --platform=linux/arm64 +image-rootfs/rootfs /build/image
  # With docker is required for loop devices
  WITH DOCKER --allow-privileged
    RUN /build-arm-image.sh --model $MODEL --directory "/build/image" /build/$IMAGE_NAME
  END
  RUN xz -v /build/$IMAGE_NAME
  SAVE ARTIFACT /build/$IMAGE_NAME.xz img AS LOCAL build/$IMAGE_NAME.xz
  SAVE ARTIFACT /build/$IMAGE_NAME.sha256 img-sha256 AS LOCAL build/$IMAGE_NAME.sha256

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
    RUN /trivy filesystem --skip-dirs /tmp --format sarif -o report.sarif --no-progress /
    RUN /trivy filesystem --skip-dirs /tmp --format template --template "@/contrib/html.tpl" -o report.html --no-progress /
    RUN /trivy filesystem --skip-dirs /tmp -f json -o results.json --no-progress /
    SAVE ARTIFACT /build/report.sarif report.sartif AS LOCAL build/${VARIANT}-${FLAVOR}-${VERSION}-trivy.sarif
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

linux-bench:
    ARG GO_VERSION
    FROM golang:$GO_VERSION
    GIT CLONE https://github.com/aquasecurity/linux-bench /linux-bench-src
    RUN cd /linux-bench-src && CGO_ENABLED=0 go build -o linux-bench . && mv linux-bench /
    SAVE ARTIFACT /linux-bench /linux-bench

# The target below should run on a live host instead. 
# However, some checks are relevant as well at container level.
# It is good enough for a quick assessment.
linux-bench-scan:
    FROM +image
    GIT CLONE https://github.com/aquasecurity/linux-bench /build/linux-bench
    WORKDIR /build/linux-bench
    COPY +linux-bench/linux-bench /build/linux-bench/linux-bench
    RUN /build/linux-bench/linux-bench


###
### Test targets
###
# usage e.g. ./earthly.sh +run-qemu-datasource-tests --FLAVOR=alpine-opensuse-leap --FROM_ARTIFACTS=true
run-qemu-datasource-tests:
    FROM +ginkgo
    RUN apt install -y qemu-system-x86 qemu-utils golang git
    WORKDIR /test
    ARG FLAVOR
    ARG PREBUILT_ISO
    ARG TEST_SUITE=autoinstall-test
    ENV FLAVOR=$FLAVOR
    ENV SSH_PORT=60023
    ENV CREATE_VM=true
    ARG CLOUD_CONFIG="./tests/assets/autoinstall.yaml"
    ENV USE_QEMU=true

    ENV GOPATH="/go"

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

    RUN PATH=$PATH:$GOPATH/bin ginkgo -v --label-filter "$TEST_SUITE" --fail-fast -r ./tests/


run-qemu-netboot-test:
    FROM +ginkgo
    COPY . /test
    WORKDIR /test

    ARG ISO_NAME=${OS_ID}
    COPY +version/VERSION ./
    ARG VERSION=$(cat VERSION)

    RUN apt update
    RUN apt install -y qemu qemu-utils qemu-system git && apt clean

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
    ENV GOPATH="/go"


    # TODO: use --pull or something to cache the python image in Earthly
    WITH DOCKER
        RUN docker run -d -v $PWD/build:/build --workdir=/build \
            --net=host -it python:3.11.0-bullseye python3 -m http.server 80 && \
            PATH=$PATH:$GOPATH/bin ginkgo --label-filter "$TEST_SUITE" --fail-fast -r ./tests/
    END

run-qemu-test:
    FROM +ginkgo
    RUN apt install -y qemu-system-x86 qemu-utils git && apt clean
    ARG FLAVOR
    ARG TEST_SUITE=upgrade-with-cli
    ARG PREBUILT_ISO
    ARG CONTAINER_IMAGE
    ENV CONTAINER_IMAGE=$CONTAINER_IMAGE
    ENV FLAVOR=$FLAVOR
    ENV SSH_PORT=60022
    ENV CREATE_VM=true
    ENV USE_QEMU=true

    ENV GOPATH="/go"

    COPY . .
    IF [ -n "$PREBUILT_ISO" ]
        ENV ISO=/build/$PREBUILT_ISO
    ELSE
        COPY +iso/kairos.iso kairos.iso
        ENV ISO=/build/kairos.iso
    END
    RUN PATH=$PATH:$GOPATH/bin ginkgo -v --label-filter "$TEST_SUITE" --fail-fast -r ./tests/

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

webui-deps:
    FROM node:19-alpine
    COPY . .
    WORKDIR ./internal/webui/public
    RUN npm install
    SAVE ARTIFACT node_modules /node_modules AS LOCAL internal/webui/public/node_modules

docs:
    FROM node:19-bullseye
    ARG TARGETARCH

    # Install dependencies
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
    COPY +build-kairos-agent/kairos-agent /usr/bin/kairos-agent
    ARG RELEASE_VERSION=$(cat VERSION)
    RUN mkdir "docs/static/$RELEASE_VERSION"
    ARG SCHEMA_FILE="docs/static/$RELEASE_VERSION/cloud-config.json"
    RUN kairos-agent print-schema > $SCHEMA_FILE 
    SAVE ARTIFACT ./docs/static/* AS LOCAL docs/static/
