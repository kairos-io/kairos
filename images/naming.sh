#!/bin/bash


# This script accepts values as defined in .github/flavors.json
# and returns a proper artifact name for that set of values.
# It's meant to be the single point of truth for artifacts names.

setEnvVarsFromJSON() {
  export FLAVOR
  export FLAVOR_RELEASE
  export VARIANT
  export TARGETARCH
  export MODEL
  FLAVOR=$(echo "$ARTIFACT_JSON" | jq -r '.flavor | select (.!=null)')
  FLAVOR_RELEASE=$(echo "$ARTIFACT_JSON" | jq -r '.flavorRelease | select (.!=null)')
  VARIANT=$(echo "$ARTIFACT_JSON" | jq -r '.variant | select (.!=null)')
  TARGETARCH=$(echo "$ARTIFACT_JSON" | jq -r '.arch | select (.!=null)')
  MODEL=$(echo "$ARTIFACT_JSON" | jq -r '.model | select (.!=null)')
}

common_artifact_name() {
  if [ -z "$KAIROS_VERSION" ]; then
    echo 'KAIROS_VERSION must be defined'
    exit 1
  fi
  if [ -z "$FLAVOR_RELEASE" ]; then
    echo 'FLAVOR_RELEASE must be defined'
    exit 1
  fi
  if [ -z "$VARIANT" ]; then
    echo 'VARIANT must be defined'
    exit 1
  fi
  if [ -z "$TARGETARCH" ]; then
    echo 'TARGETARCH must be defined'
    exit 1
  fi
  if [ -z "$MODEL" ]; then
    echo 'MODEL must be defined'
    exit 1
  fi

  echo "$FLAVOR_RELEASE-$VARIANT-$TARGETARCH-$MODEL-$KAIROS_VERSION"
}

common_artifact_base_name() {
  if [ -z "$FLAVOR_RELEASE" ]; then
    echo 'FLAVOR_RELEASE must be defined'
    exit 1
  fi
  if [ -z "$TARGETARCH" ]; then
    echo 'TARGETARCH must be defined'
    exit 1
  fi
  if [ -z "$MODEL" ]; then
    echo 'MODEL must be defined'
    exit 1
  fi

  echo "$FLAVOR_RELEASE-$TARGETARCH-$MODEL"
}

bootable_artifact_name() {
  if [ -z "$FLAVOR" ]; then
    echo 'FLAVOR must be defined'
    exit 1
  fi
  local common
  common=$(common_artifact_name)

  echo "kairos-$FLAVOR-$common"
}

container_artifact_name() {
  if [ -z "$KAIROS_VERSION" ]; then
    echo 'KAIROS_VERSION must be defined'
    exit 1
  fi

  if [ -z "$FLAVOR" ]; then
    echo 'FLAVOR must be defined'
    exit 1
  fi

  if [ -z "$REGISTRY_AND_ORG" ]; then
    echo 'REGISTRY_AND_ORG must be defined'
    exit 1
  fi

  # quay.io doesn't accept "+" in the repo name
  export KAIROS_VERSION="${KAIROS_VERSION/+/-}"
  local tag
  tag=$(common_artifact_name)

  echo "$REGISTRY_AND_ORG/$FLAVOR:$tag"
}

container_artifact_base_name() {
  if [ -z "$BRANCH" ]; then
    export BRANCH=master
  fi

  if [ -z "$FLAVOR" ]; then
    echo 'FLAVOR must be defined'
    exit 1
  fi

  if [ -z "$REGISTRY_AND_ORG" ]; then
    echo 'REGISTRY_AND_ORG must be defined'
    exit 1
  fi

  # quay.io doesn't accept "+" in the repo name
  export KAIROS_VERSION="${KAIROS_VERSION/+/-}"
  local tag
  tag=$(common_artifact_base_name)

  echo "$REGISTRY_AND_ORG/$FLAVOR:$tag-$BRANCH"
}

container_artifact_label() {
  if [ -z "$KAIROS_VERSION" ]; then
    echo 'KAIROS_VERSION must be defined'
    exit 1
  fi

  export KAIROS_VERSION="${KAIROS_VERSION/+/-}"
  common_artifact_name
}

# returns the repo name for the container artifact
# for example quay.io/kairos/opensuse or quake.io/kairos/alpine
container_artifact_repo() {
  if [ -z "$FLAVOR" ]; then
    echo 'FLAVOR must be defined'
    exit 1
  fi

  if [ -z "$REGISTRY_AND_ORG" ]; then
      echo 'REGISTRY_AND_ORG must be defined'
      exit 1
  fi

  echo "$REGISTRY_AND_ORG/$FLAVOR"
}


if [ -n "$ARTIFACT_JSON" ]; then
  setEnvVarsFromJSON
fi

case "$1" in
  "container_artifact_name")
    container_artifact_name
    ;;
  "container_artifact_label")
    container_artifact_label
    ;;
  "bootable_artifact_name")
    bootable_artifact_name
    ;;
  "common_artifact_name")
    common_artifact_name
    ;;
  "container_artifact_repo")
    container_artifact_repo
    ;;
  "container_artifact_base_name")
    container_artifact_base_name
    ;;
  *)
    echo "Function not found: $1"
    exit 1
    ;;
esac

# ARTIFACT_JSON='{"flavor":"opensuse-leap","flavorRelease":"15.5","variant":"standard","model":"generic","arch":"amd64"}'
# KAIROS_VERSION=v2.4.1
# REGISTRY_AND_ORG=quay.io/kairos
# container_artifact_name
# bootable_artifact_name
