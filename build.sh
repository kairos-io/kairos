#!/bin/bash

set -ex

IMAGE="${IMAGE:-c3os}"
ISO="${ISO:-c3os}"
FLAVOR="${FLAVOR:-opensuse}"
C3OS_VERSION="${C3OS_VERSION:-c3OS22}"
K3S_VERSION="${K3S_VERSION:-v1.21.4+k3s1}"

docker build --build-arg C3OS_VERSION=$C3OS_VERSION \
             --build-arg K3S_VERSION=$K3S_VERSION \
             -t $IMAGE \
             -f Dockerfile.${FLAVOR} ./

docker run -v $PWD:/cOS \
           -v /var/run:/var/run \
           --entrypoint /usr/bin/luet-makeiso \
           -i --rm quay.io/costoolkit/toolchain ./iso.yaml --image $IMAGE --output $ISO