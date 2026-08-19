#!/bin/bash

set -e

# This scripts prepares a cluster where we install the kcrypt CRDs.
# This is where sealed volumes are created.

GINKGO_NODES="${GINKGO_NODES:-1}"
K3S_IMAGE="rancher/k3s:v1.26.1-k3s1"
CERT_MANAGER_VERSION="v1.16.5"

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
CLUSTER_NAME=$(echo $RANDOM | md5sum | head -c 10; echo;)
export KUBECONFIG=$(mktemp)

# https://unix.stackexchange.com/a/423052
getFreePort() {
  echo $(comm -23 <(seq "30000" "30200" | sort) <(ss -Htan | awk '{print $4}' | cut -d':' -f2 | sort -u) | shuf | head -n "1")
}

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

# Import the controller image that we built at the start into to the cluster
# this image has to exists and be available in the local docker
k3d image import -c "$CLUSTER_NAME" controller:latest

# Install cert manager
kubectl apply -f "https://github.com/jetstack/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
kubectl wait --for=condition=Available deployment --timeout=2m -n cert-manager --all

# Replace the CLUSTER_IP in the kustomize resource
# Only needed for debugging so that we can access the server from the host
# (the 10.0.2.2 IP address is only useful from within qemu)
export CLUSTER_IP=$(docker inspect "k3d-${CLUSTER_NAME}-server-0"  | jq -r '.[0].NetworkSettings.Networks[].IPAddress')
envsubst \
    < "$SCRIPT_DIR/../tests/assets/challenger-server-ingress.template.yaml" \
    > "$SCRIPT_DIR/../tests/assets/challenger-server-ingress.yaml"

# Install the challenger server kustomization
kubectl apply -k "$SCRIPT_DIR/../tests/assets/"
kubectl wait --for=condition=Available deployment/kcrypt-controller-controller-manager -n default --timeout=2m

# 10.0.2.2 is where the vm sees the host
# https://stackoverflow.com/a/6752280
export KMS_ADDRESS="10.0.2.2.challenger.sslip.io"

# The tpm emulator needs CGO
# https://github.com/google/go-tpm-tools/blob/215e2ab8d3ee0a9aab1249e908313c2ecddd692e/simulator/internal/internal_cross.go#L19
export CGO_ENABLED=1

go run github.com/onsi/ginkgo/v2/ginkgo -v --nodes $GINKGO_NODES --label-filter $LABEL --fail-fast -r ./tests/
