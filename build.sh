#!/bin/bash

set -ex

IMAGE="${IMAGE:-c3os}"
ISO="${ISO:-c3os}"

docker build -t $IMAGE .
docker run -v $PWD:/cOS \
           -v /var/run:/var/run \
           --entrypoint /usr/bin/luet-makeiso \
           -ti --rm quay.io/costoolkit/toolchain ./iso.yaml --image $IMAGE --output $ISO