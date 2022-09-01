#!/bin/bash
set -e

root_dir=$(git rev-parse --show-toplevel)

last_snapshot() {
    echo $(docker run --rm quay.io/skopeo/stable list-tags docker://$1 | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) and ( . | contains("v")))) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' | sed "s/-repository.yaml//g")
}

YQ=${YQ:-docker run --rm -v "${PWD}":/workdir mikefarah/yq}
set -x

latest_tag=$(last_snapshot quay.io/costoolkit/releases-teal)
latest_tag_arm64=$(last_snapshot quay.io/costoolkit/releases-teal-arm64)

$YQ eval '.repositories[0].reference = "'$latest_tag'-repository.yaml"' -i repositories/repositories.yaml
$YQ eval '.repositories[1].reference = "'$latest_tag_arm64'-repository.yaml"' -i repositories/repositories.yaml

latest_tag_blue=$(last_snapshot quay.io/costoolkit/releases-blue)
latest_tag_blue_arm64=$(last_snapshot quay.io/costoolkit/releases-blue-arm64)

$YQ eval '.repositories[0].reference = "'$latest_tag_blue'-repository.yaml"' -i repositories/repositories.yaml.fedora
$YQ eval '.repositories[1].reference = "'$latest_tag_blue_arm64'-repository.yaml"' -i repositories/repositories.yaml.fedora

latest_tag_orange=$(last_snapshot quay.io/costoolkit/releases-orange)
latest_tag_orange_arm64=$(last_snapshot quay.io/costoolkit/releases-orange-arm64)

$YQ eval '.repositories[0].reference = "'$latest_tag_orange'-repository.yaml"' -i repositories/repositories.yaml.ubuntu
$YQ eval '.repositories[1].reference = "'$latest_tag_orange_arm64'-repository.yaml"' -i repositories/repositories.yaml.ubuntu

latest_tag=$(last_snapshot quay.io/costoolkit/releases-green)
latest_tag_arm64=$(last_snapshot quay.io/costoolkit/releases-green-arm64)

$YQ eval '.repositories[0].reference = "'$latest_tag'-repository.yaml"' -i repositories/repositories.yaml.tumbleweed
$YQ eval '.repositories[1].reference = "'$latest_tag_arm64'-repository.yaml"' -i repositories/repositories.yaml.tumbleweed

last_commit_snapshot() {
    echo $(docker run --rm quay.io/skopeo/stable list-tags docker://$1 | jq -rc '.Tags | map(select( (. | contains("-repository.yaml")) )) | sort_by(. | sub("v";"") | sub("-repository.yaml";"") | sub("-";"") | split(".") | map(tonumber) ) | .[-1]' | sed "s/-repository.yaml//g")
}

latest_tag=$(last_commit_snapshot quay.io/c3os/packages)
latest_tag_arm64=$(last_commit_snapshot quay.io/c3os/packages-arm64)

for REPOFILE in repositories.yaml.tumbleweed repositories.yaml.ubuntu repositories.yaml.fedora repositories.yaml
do
    $YQ eval '.repositories[2].reference = "'$latest_tag'-repository.yaml"' -i repositories/$REPOFILE
    $YQ eval '.repositories[3].reference = "'$latest_tag_arm64'-repository.yaml"' -i repositories/$REPOFILE
done

