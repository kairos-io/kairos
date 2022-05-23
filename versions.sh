#!/bin/bash

GIT_TAG="${TAG:-$(git describe --tags --abbrev=0 --exact-match --dirty)}"

if [ -z "$GIT_TAG" ]; then
  echo "Dirty tag"
  GIT_TAG="$(git describe --tags --abbrev=0)dev"
fi

echo "Git tag describe: $GIT_TAG"
gt=(${GIT_TAG//-/ })

internal_version=${gt[1]}
k8s_version=${gt[0]}

OS_ID=${OS_ID:-c3os}
IMAGE="${IMAGE:-${OS_ID}:latest}"
ISO="${ISO:-$OS_ID}"
FLAVOR="${FLAVOR:-opensuse}"
C3OS_VERSION="${C3OS_VERSION:--c3OS$internal_version}"
K3S_VERSION="${K3S_VERSION:-$k8s_version+k3s1}"
OS_LABEL="${OS_LABEL:-$FLAVOR-latest}"
OS_NAME="${OS_NAME:-$OS_ID-$FLAVOR}"
LUET_VERSION="${LUET_VERSION:-0.22.7-1}"

echo "c3os version: $C3OS_VERSION"
echo "k3s version: $K3S_VERSION"
