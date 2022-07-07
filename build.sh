#!/bin/bash

TARGET=${1:-all}
shift
source versions.sh

set -ex

echo "Building $ISO from $IMAGE"

CMD=./earthly.sh
if hash earthly 2>/dev/null; then
    CMD=earthly
fi

$CMD $@ +${TARGET} \
             --FLAVOR=$FLAVOR \
             --IMAGE=$IMAGE \
             --IMAGE_NAME=$IMAGE_NAME \
             --MODEL=$MODEL \
             --ISO_NAME=${ISO:-c3os-$FLAVOR} \
             --LUET_VERSION=$LUET_VERSION \
             --C3OS_VERSION=$internal_version \
             --K3S_VERSION=$k8s_version \
             --OS_LABEL=$OS_LABEL \
             --OS_ID=$OS_ID \
             --OS_NAME=$OS_NAME

