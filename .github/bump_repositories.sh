#!/bin/bash
set -e

root_dir=$(git rev-parse --show-toplevel)

last_snapshot() {
    echo $(docker run --rm quay.io/skopeo/stable list-tags docker://$1 | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) and ( . | contains("v")))) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' | sed "s/-repository.yaml//g")
}

reference() {
    nr=$1
    tag=$2

    echo ".repositories[$nr] |= . * { \"reference\": \"$tag-repository.yaml\" }"
}

YQ=${YQ:-docker run --rm -v "${PWD}":/workdir mikefarah/yq}
set -x

latest_tag=$(last_snapshot quay.io/costoolkit/releases-teal)
latest_tag_arm64=$(last_snapshot quay.io/costoolkit/releases-teal-arm64)

$YQ eval "$(reference 0 $latest_tag)" -i repositories/repositories.yaml
$YQ eval "$(reference 1 $latest_tag_arm64)" -i repositories/repositories.yaml

latest_tag_blue=$(last_snapshot quay.io/costoolkit/releases-blue)
latest_tag_blue_arm64=$(last_snapshot quay.io/costoolkit/releases-blue-arm64)

$YQ eval "$(reference 0 $latest_tag_blue)" -i repositories/repositories.yaml.fedora
$YQ eval "$(reference 1 $latest_tag_blue_arm64)" -i repositories/repositories.yaml.fedora
$YQ eval "$(reference 0 $latest_tag_blue)" -i repositories/repositories.yaml.rockylinux
$YQ eval "$(reference 1 $latest_tag_blue_arm64)" -i repositories/repositories.yaml.rockylinux

latest_tag_orange=$(last_snapshot quay.io/costoolkit/releases-orange)
latest_tag_orange_arm64=$(last_snapshot quay.io/costoolkit/releases-orange-arm64)

$YQ eval "$(reference 0 $latest_tag_orange)" -i repositories/repositories.yaml.ubuntu
$YQ eval "$(reference 1 $latest_tag_orange_arm64)" -i repositories/repositories.yaml.ubuntu

latest_tag=$(last_snapshot quay.io/costoolkit/releases-green)
latest_tag_arm64=$(last_snapshot quay.io/costoolkit/releases-green-arm64)

$YQ eval "$(reference 0 $latest_tag)" -i repositories/repositories.yaml.tumbleweed
$YQ eval "$(reference 1 $latest_tag_arm64)" -i repositories/repositories.yaml.tumbleweed

last_commit_snapshot() {
    echo $(docker run --rm quay.io/skopeo/stable list-tags docker://$1 | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' | sed "s/-repository.yaml//g")
}

latest_tag=$(last_commit_snapshot quay.io/kairos/packages)
latest_tag_arm64=$(last_commit_snapshot quay.io/kairos/packages-arm64)

for REPOFILE in repositories.yaml.tumbleweed repositories.yaml.rockylinux repositories.yaml.ubuntu repositories.yaml.fedora repositories.yaml
do
    $YQ eval "$(reference 2 $latest_tag)" -i repositories/$REPOFILE
    $YQ eval "$(reference 3 $latest_tag_arm64)" -i repositories/$REPOFILE
done

