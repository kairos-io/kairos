#!/bin/bash
set -ex

pushd image-bundle
    docker build -t bundle .
popd

if [ ! -d data ]; then
 mkdir data
fi

pushd data
    docker save bundle -o bundle.tar
popd

docker run -v $PWD/config.yaml:/config.yaml \
             -v $PWD/build:/tmp/auroraboot \
             -v $PWD/data:/tmp/data \
             --rm -ti quay.io/kairos/auroraboot:v0.2.0 \
             --set "disable_http_server=true" \
             --set "disable_netboot=true" \
             --set "iso.data=/tmp/data" \
             --cloud-config /config.yaml \
             --set "state_dir=/tmp/auroraboot" \
             --set "artifact_version=v1.6.1-k3sv1.26.1+k3s1" \
             --set "release_version=v1.6.1" \
             --set "flavor=fedora" \
             --set "repository=kairos-io/provider-kairos"

echo "Custom ISO ready at $PWD/build/kairos.iso.custom.iso"
