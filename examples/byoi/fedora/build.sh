#!/bin/bash

set -ex

# Build the container image
docker build -t test-byoi .

# Create an ISO
docker run -v $PWD/build:/tmp/auroraboot \
             -v /var/run/docker.sock:/var/run/docker.sock \
             --rm -ti quay.io/kairos/auroraboot:v0.2.2 \
             --set container_image=docker://test-byoi \
             --set "disable_http_server=true" \
             --set "disable_netboot=true" \
             --set "state_dir=/tmp/auroraboot"