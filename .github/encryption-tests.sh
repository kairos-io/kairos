#!/bin/bash

set -ex

# This scripts prepares a cluster where we install the kcrypt CRDs.
# This is where sealed volumes are created.

GINKGO_NODES="${GINKGO_NODES:-1}"
K3S_IMAGE="rancher/k3s:v1.26.1-k3s1"

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
CLUSTER_NAME=$(echo $RANDOM | md5sum | head -c 10; echo;)
KUBECONFIG=$(mktemp)
export KUBECONFIG

cleanup() {
  echo "Cleaning up $CLUSTER_NAME"
  k3d cluster delete "$CLUSTER_NAME" || true
  rm -rf "$KUBECONFIG"
}
trap cleanup EXIT

# Create a cluster and bind ports 80 and 443 on the host
# This will allow us to access challenger server on 10.0.2.2 which is the IP
# on which qemu "sees" the host.
# We change the CIDR because k3s creates iptables rules that block DNS traffic to this CIDR
# (something like that). If you run k3d inside a k3s cluster (inside a Pod), DNS won't work
# inside the k3d server container unless you use a different CIDR.
# Here we are avoiding CIDR "10.43.x.x"
k3d cluster create "$CLUSTER_NAME" --k3s-arg "--cluster-cidr=10.49.0.1/16@server:0" --k3s-arg "--service-cidr=10.48.0.1/16@server:0" -p '80:80@server:0' -p '443:443@server:0' --image "$K3S_IMAGE"
k3d kubeconfig get "$CLUSTER_NAME" > "$KUBECONFIG"

# Import the image to the cluster
#docker pull quay.io/kairos/kcrypt-challenger:latest
#k3d image import -c "$CLUSTER_NAME" quay.io/kairos/kcrypt-challenger:latest

# Install cert manager
kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Available deployment --timeout=2m -n cert-manager --all

# Replace the CLUSTER_IP in the kustomize resource
# Only needed for debugging so that we can access the server from the host
# (the 10.0.2.2 IP address is only useful from within qemu)
CLUSTER_IP=$(docker inspect "k3d-${CLUSTER_NAME}-server-0"  | jq -r '.[0].NetworkSettings.Networks[].IPAddress')
export CLUSTER_IP

envsubst \
    < "$SCRIPT_DIR/../tests/assets/encryption/challenger-server-ingress.template.yaml" \
    > "$SCRIPT_DIR/../tests/assets/encryption/challenger-server-ingress.yaml"

# Install the challenger server kustomization
kubectl apply -k "$SCRIPT_DIR/../tests/assets/encryption/"

# 10.0.2.2 is where the vm sees the host
# https://stackoverflow.com/a/6752280
export KMS_ADDRESS="10.0.2.2.challenger.sslip.io"

pushd "$SCRIPT_DIR/../tests/"
go run github.com/onsi/ginkgo/v2/ginkgo -v --nodes "$GINKGO_NODES" --label-filter "$LABEL" --fail-fast -r ./...
popd
