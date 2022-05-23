#!/bin/bash

source versions.sh

set -ex

docker build --build-arg C3OS_VERSION=$C3OS_VERSION \
             --build-arg K3S_VERSION=$K3S_VERSION \
             --build-arg LUET_VERSION=$LUET_VERSION \
             --build-arg OS_LABEL=$OS_LABEL \
             --build-arg OS_NAME=$OS_NAME \
             -t $IMAGE \
             -f images/Dockerfile.${FLAVOR} ./

docker run -v $PWD:/cOS \
           -v /var/run:/var/run \
           --entrypoint /usr/bin/luet-makeiso \
           -i --rm quay.io/costoolkit/toolchain:0.8.7-16-gdcaac339 ./iso.yaml --image $IMAGE --output $ISO
