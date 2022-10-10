#!/bin/bash

curl https://luet.io/install.sh | sudo sh

sudo luet repo add kairos --yes --url quay.io/kairos/packages --type docker
sudo luet install -y utils/goreleaser utils/earthly