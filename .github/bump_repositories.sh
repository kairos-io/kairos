#!/bin/bash
set -e

root_dir=$(git rev-parse --show-toplevel)

reference() {
    nr=$1
    tag=$2

    echo ".repositories[$nr] |= . * { \"reference\": \"$tag-repository.yaml\" }"
}

YQ=${YQ:-docker run --rm -v "${PWD}":/workdir mikefarah/yq}
set -x

last_commit_snapshot() {
    echo $(docker run --rm quay.io/skopeo/stable list-tags docker://$1 | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' | sed "s/-repository.yaml//g")
}

latest_tag=$(last_commit_snapshot quay.io/kairos/packages)
latest_tag_arm64=$(last_commit_snapshot quay.io/kairos/packages-arm64)

for REPOFILE in repositories.yaml
do
    $YQ eval "$(reference 0 $latest_tag)" -i repositories/$REPOFILE
    $YQ eval "$(reference 1 $latest_tag_arm64)" -i repositories/$REPOFILE
done

