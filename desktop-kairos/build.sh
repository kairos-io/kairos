#!/bin/bash

export SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
export IMAGE=kairos-desktop

echo "Building the image"
docker build -t "$IMAGE" -f "$SCRIPT_DIR/Dockerfile" "$SCRIPT_DIR"

echo "Writing the config.yaml file"
cat << EOF > $SCRIPT_DIR/config.yaml
#cloud-config
users:
  - name: kairos
    passwd: kairos

install:
   auto: true
   device: "auto"
   reboot: true

k3s:
  enabled: true
EOF

echo "Building the ISO"
docker run -v "$SCRIPT_DIR"/config.yaml:/config.yaml \
             -v "$SCRIPT_DIR"/build:/tmp/auroraboot \
             -v /var/run/docker.sock:/var/run/docker.sock \
             --rm -ti quay.io/kairos/auroraboot \
             --set container_image="docker://$IMAGE" \
             --set "disable_http_server=true" \
             --set "disable_netboot=true" \
             --cloud-config /config.yaml \
             --set "state_dir=/tmp/auroraboot"

docker run -v "$SCRIPT_DIR"/build:/tmp/build $IMAGE chown -R 1000:1001 /tmp/build
