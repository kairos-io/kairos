#!/bin/bash
set -e

root_dir=$(git rev-parse --show-toplevel)

last_snapshot() {
    echo $(skopeo list-tags docker://$1 | jq -rc '.Tags | map(select(. | contains("-repository.yaml"))) | sort | .[-1]' | sed "s/-repository.yaml//g")
}

YQ=${YQ:-yq}

latest_tag=$(last_snapshot quay.io/costoolkit/releases-green)
latest_tag_arm64=$(last_snapshot quay.io/costoolkit/releases-green-arm64)

latest_tag_blue=$(last_snapshot quay.io/costoolkit/releases-blue)
latest_tag_blue_arm64=$(last_snapshot quay.io/costoolkit/releases-blue-arm64)

latest_tag_orange=$(last_snapshot quay.io/costoolkit/releases-orange)
latest_tag_orange_arm64=$(last_snapshot quay.io/costoolkit/releases-orange-arm64)

$YQ eval '.repositories[0].reference = "'$latest_tag'-repository.yaml"' -i $root_dir/repositories.yaml
$YQ eval '.repositories[1].reference = "'$latest_tag_arm64'-repository.yaml"' -i $root_dir/repositories.yaml

$YQ eval '.repositories[0].reference = "'$latest_tag_blue'-repository.yaml"' -i $root_dir/repositories.yaml.fedora
$YQ eval '.repositories[1].reference = "'$latest_tag_blue_arm64'-repository.yaml"' -i $root_dir/repositories.yaml.fedora

$YQ eval '.repositories[0].reference = "'$latest_tag_orange'-repository.yaml"' -i $root_dir/repositories.yaml.ubuntu
$YQ eval '.repositories[1].reference = "'$latest_tag_orange_arm64'-repository.yaml"' -i $root_dir/repositories.yaml.ubuntu