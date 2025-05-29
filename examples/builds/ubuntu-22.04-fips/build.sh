#!/bin/bash

set -ex

# Build the container image
docker build --secret id=pro-attach-config,src=pro-attach-config.yaml -t ubuntu-jammy-fips .

# Build ISO from that container
docker run --rm -ti \
-v "$PWD"/build:/tmp/auroraboot \
-v /var/run/docker.sock:/var/run/docker.sock \
quay.io/kairos/auroraboot:v0.5.0 \
--set container_image=docker://ubuntu-jammy-fips \
--set "disable_http_server=true" \
--set "disable_netboot=true" \
--set "state_dir=/tmp/auroraboot"
