#!/bin/bash

curl https://luet.io/install.sh | sudo sh

sudo luet repo add kairos --yes --url quay.io/kairos/packages --type docker
sudo luet install -y utils/goreleaser utils/operator-sdk utils/kubesplit

wget -q -O - https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
