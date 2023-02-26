#!/bin/bash
set -e

reference() {
    nr=$1
    tag=$2

    echo ".repositories[$nr] |= . * { \"reference\": \"$tag-repository.yaml\" }"
}

YQ=${YQ:-docker run --rm -v "${PWD}":/workdir mikefarah/yq}
set -x

last_commit_snapshot() {
    docker run --rm quay.io/skopeo/stable list-tags docker://"${1}" | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' | sed "s/-repository.yaml//g"
}

latest_tag=$(last_commit_snapshot quay.io/kairos/packages)
latest_tag_arm64=$(last_commit_snapshot quay.io/kairos/packages-arm64)

# shellcheck disable=SC2043
for REPOFILE in framework-profile.yaml
do
    "${YQ}" eval "$(reference 0 "${latest_tag}")" -i "${REPOFILE}"
    "${YQ}" eval "$(reference 1 "${latest_tag_arm64}")" -i "${REPOFILE}"
done

