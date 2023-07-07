#!/bin/bash

set -ex

# Build the container image
docker build -t test-byoi-fips .

docker run -v "$PWD"/build:/tmp/auroraboot \
        -v /var/run/docker.sock:/var/run/docker.sock \
        --rm -ti quay.io/kairos/auroraboot \
        --set container_image=docker://test-byoi-fips \
        --set "disable_http_server=true" \
        --set "disable_netboot=true" \
        --set "state_dir=/tmp/auroraboot"
