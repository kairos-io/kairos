#!/bin/bash

set -ex

OS_ID=${OS_ID:-c3os}
IMAGE="${IMAGE:-$OS_ID}"
ISO="${ISO:-$OS_ID}"
FLAVOR="${FLAVOR:-opensuse}"
C3OS_VERSION="${C3OS_VERSION:--c3OS28}"
K3S_VERSION="${K3S_VERSION:-v1.21.4+k3s1}"
OS_LABEL="${OS_LABEL:-$FLAVOR-latest}"
OS_NAME="${OS_NAME:-$OS_ID-$FLAVOR}"
LUET_VERSION="${LUET_VERSION:-0.22.7-1}"
docker build --build-arg C3OS_VERSION=$C3OS_VERSION \
             --build-arg K3S_VERSION=$K3S_VERSION \
             --build-arg LUET_VERSION=$LUET_VERSION \
             --build-arg OS_LABEL=$OS_LABEL \
             --build-arg OS_NAME=$OS_NAME \
             -t $IMAGE \
             -f Dockerfile.${FLAVOR} ./

docker run -v $PWD:/cOS \
           -v /var/run:/var/run \
           --entrypoint /usr/bin/luet-makeiso \
           -i --rm quay.io/costoolkit/toolchain ./iso.yaml --image $IMAGE --output $ISO
