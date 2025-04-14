#!/bin/bash

# This script tests a GCP image by launching a test instance and verifying it boots correctly.
# It will clean up all resources after the test, regardless of success or failure.

set -e
set -o pipefail

# Generate a unique instance name using timestamp and random string
generateInstanceName() {
  timestamp=$(date +%Y%m%d%H%M%S)
  random=$(head /dev/urandom | tr -dc 'a-z0-9' | head -c 4)
  echo "test-kairos-instance-${timestamp}-${random}"
}

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
  # Delete the instance and its boot disk
  gcloud compute instances delete "$INSTANCE_NAME" \
    --project="$GCP_PROJECT" \
    --zone=europe-west3-a \
    --delete-disks=boot \
    --quiet || true

  # Remove temporary SSH key
  rm -f "$TEMP_KEY_FILE" "${TEMP_KEY_FILE}.pub" || true
}

# Ensure cleanup runs even if the script fails
trap cleanup EXIT

# ----- Main script -----
checkEnvVars
checkArguments "$@"

imageName="$1"
INSTANCE_NAME=$(generateInstanceName)
TEMP_KEY_FILE="/tmp/${INSTANCE_NAME}.pem"

echo "Testing GCP image: $imageName"
echo "Using instance name: $INSTANCE_NAME"

# Generate temporary SSH key pair
echo "Generating temporary SSH key pair..."
ssh-keygen -t rsa -b 2048 -f "$TEMP_KEY_FILE" -N "" -q
chmod 600 "$TEMP_KEY_FILE"

# Create userdata configuration with SSH key
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
    ssh_authorized_keys:
      - "$(cat "${TEMP_KEY_FILE}.pub")"
EOF
)

# Create a test instance
echo "Launching test instance..."
gcloud compute instances create "$INSTANCE_NAME" \
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
  status=$(gcloud compute instances describe "$INSTANCE_NAME" \
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

  if [ "$i" -eq 30 ]; then
    echo "Timeout waiting for instance to start"
    exit 1
  fi

  sleep 10
done

# Wait for Kairos agent to be ready and check its state
echo "Waiting for Kairos agent to be ready..."
for i in {1..60}; do
  # Try to run the command on the instance using gcloud compute ssh with the private key
  output=$(gcloud compute ssh kairos@"$INSTANCE_NAME" \
    --project="$GCP_PROJECT" \
    --zone=europe-west3-a \
    --strict-host-key-checking=no \
    --command="kairos-agent state get boot" \
    --ssh-key-file="$TEMP_KEY_FILE" \
    --ssh-flag="-o PasswordAuthentication=no" \
    --ssh-flag="-o PreferredAuthentications=publickey" \
    --ssh-flag="-o ConnectTimeout=5" 2>&1 | tail -n 1 || true)

  if [ "$output" == "active_boot" ]; then
    echo "Kairos agent is in active_boot state!"
    break
  else
    echo "Attempt $i/60 failed: $output"
    echo "Will retry in 10 seconds..."
  fi

  if [ "$i" -eq 60 ]; then
    echo "Timeout waiting for Kairos agent to reach active_boot state"
    exit 1
  fi

  sleep 10
done

echo "Image test completed successfully!" 