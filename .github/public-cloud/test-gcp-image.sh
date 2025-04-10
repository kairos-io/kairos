#!/bin/bash

# This script tests a GCP image by launching a test instance and verifying it boots correctly.
# It will clean up all resources after the test, regardless of success or failure.

set -e
set -o pipefail

checkArguments() {
  if [ $# -lt 1 ]; then
    echo "Error: You need to specify the GCP image name to test."
    echo "Usage: $0 <image-name>"
    exit 1
  fi
}

checkEnvVars() {
  if [ -z "$GCP_PROJECT" ]; then
    echo "Error: GCP_PROJECT environment variable must be set."
    exit 1
  fi
}

cleanup() {
  echo "Cleaning up test resources..."
  gcloud compute instances delete test-kairos-instance \
    --project="$GCP_PROJECT" \
    --zone=europe-west3-a \
    --quiet || true
}

# Ensure cleanup runs even if the script fails
trap cleanup EXIT

# ----- Main script -----
checkEnvVars
checkArguments "$@"

imageName="$1"

echo "Testing GCP image: $imageName"

# Create userdata configuration
userdata=$(cat <<EOF
#cloud-config
install:
  auto: true
  reboot: true
  device: auto
  poweroff: false
users:
  - name: kairos
    groups:
    - admin
EOF
)

# Create a test instance
echo "Launching test instance..."
gcloud compute instances create test-kairos-instance \
  --project="$GCP_PROJECT" \
  --zone=europe-west3-a \
  --machine-type=e2-small \
  --image="$imageName" \
  --boot-disk-size=40GB \
  --boot-disk-type=pd-standard \
  --metadata-from-file=user-data=<(echo "$userdata")

# Wait for the instance to start and check its status
echo "Waiting for instance to start..."
for i in {1..30}; do
  status=$(gcloud compute instances describe test-kairos-instance \
    --project="$GCP_PROJECT" \
    --zone=europe-west3-a \
    --format="value(status)" 2>/dev/null || echo "NOT_FOUND")

  if [ "$status" == "RUNNING" ]; then
    echo "Instance is running successfully!"
    break
  elif [ "$status" == "TERMINATED" ] || [ "$status" == "STOPPED" ]; then
    echo "Instance failed to start properly. Status: $status"
    exit 1
  fi

  if [ $i -eq 30 ]; then
    echo "Timeout waiting for instance to start"
    exit 1
  fi

  sleep 10
done

# Wait for Kairos agent to be ready and check its state
echo "Waiting for Kairos agent to be ready..."
for i in {1..60}; do
  # Try to run the command on the instance
  if gcloud compute ssh test-kairos-instance \
    --project="$GCP_PROJECT" \
    --zone=europe-west3-a \
    --command="kairos-agent state get boot" 2>/dev/null | grep -q "active_boot"; then
    echo "Kairos agent is in active_boot state!"
    break
  fi

  if [ $i -eq 60 ]; then
    echo "Timeout waiting for Kairos agent to reach active_boot state"
    exit 1
  fi

  sleep 10
done

echo "Image test completed successfully!" 