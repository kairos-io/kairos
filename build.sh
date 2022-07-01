#!/bin/bash

source versions.sh

set -ex

echo "Building $ISO from $IMAGE"

docker build --build-arg C3OS_VERSION=$C3OS_VERSION \
             --build-arg K3S_VERSION=$K3S_VERSION \
             --build-arg LUET_VERSION=$LUET_VERSION \
             --build-arg OS_LABEL=$OS_LABEL \
             --build-arg OS_NAME=$OS_NAME \
             --build-arg https_proxy \
             --build-arg http_proxy \
             --build-arg no_proxy \
             -t $IMAGE \
             -f images/Dockerfile.${FLAVOR} ./

docker run -v $PWD:/cOS \
           -v /var/run:/var/run \
           -e https_proxy -e http_proxy -e no_proxy \
           -i --rm quay.io/costoolkit/elemental:v0.0.15-8a78e6b --name $ISO --debug build-iso --date=false --local --overlay-iso /cOS/overlay/files-iso $IMAGE --output /cOS/

# See: https://github.com/rancher/elemental-cli/issues/228
sha256sum $ISO.iso > $ISO.iso.sha256
