#!/bin/bash

set -e
set -o pipefail

checkEnvVars() {
  if [ -z "$AZURE_RESOURCE_GROUP" ]; then
    echo "Error: AZURE_RESOURCE_GROUP environment variable must be set."
    exit 1
  fi
}

waitForInstanceStatus() {
  local instance_name="$1"
  local target_state="$2"
  local status=""

  echo "Waiting for instance $instance_name to reach state $target_state..."
  while true; do
    status=$(az vm show --resource-group "$AZURE_RESOURCE_GROUP" --name "$instance_name" \
      --query "provisioningState" --output tsv 2>/dev/null || echo "NOT_FOUND")

    if [ "$status" == "$target_state" ]; then
      echo "Instance reached state: $target_state"
      break
    elif [ "$status" == "Failed" ] || [ "$status" == "Deleting" ]; then
      echo "Instance reached terminal state '$status' while waiting for '$target_state'"
      exit 1
    else
      echo "Current instance state: $status"
      sleep 10
    fi
  done
}

waitForKairosActiveBoot() {
  local public_ip="$1"
  local key_file="$2"
  local max_attempts=60  # 10 minutes (60 * 10 seconds)
  local attempt=0
  local boot_state=""
  local ssh_error=""

  echo "Waiting for Kairos to reach active_boot state..."
  while [ $attempt -lt $max_attempts ]; do
    # Try to get the boot state via SSH with host key checking disabled
    if boot_state=$(ssh -i "$key_file" \
      -o StrictHostKeyChecking=no \
      -o UserKnownHostsFile=/dev/null \
      -o ConnectTimeout=5 \
      -o BatchMode=yes \
      -o LogLevel=ERROR \
      kairos@"$public_ip" \
      kairos-agent state get boot 2>&1); then
      # Clean the output to get just the boot state
      boot_state=$(echo "$boot_state" | grep -v "WARNING" | grep -v "Offending" | grep -v "authentication" | grep -v "host key" | tr -d '[:space:]')
      if [ "$boot_state" == "active_boot" ]; then
        echo "Kairos has successfully reached active_boot state!"
        return 0
      else
        echo "Current Kairos boot state: $boot_state"
      fi
    else
      ssh_error=$boot_state
      # Mask the IP address in the error message
      ssh_error=${ssh_error//$public_ip/[REDACTED]}
      echo "SSH connection attempt failed. Error: $ssh_error"
    fi

    attempt=$((attempt + 1))
    sleep 10
  done

  echo "Timeout waiting for Kairos to reach active_boot state"
  if [ -n "$ssh_error" ]; then
    echo "Last SSH error: $ssh_error"
  fi
  return 1
}

cleanupTestResources() {
  local test_name="$1"
  local temp_key_file="$2"

  echo "Cleaning up test resources..."
  # Delete the VM first
  echo "Deleting VM (${test_name})..."
  az vm delete --resource-group "$AZURE_RESOURCE_GROUP" --name "$test_name" --yes

  # Delete the NIC explicitly
  echo "Deleting network interface (${test_name}VMNic)..."
  az network nic delete --resource-group "$AZURE_RESOURCE_GROUP" --name "${test_name}VMNic"

  # Now we can safely delete the NSG
  echo "Deleting network security group (${test_name})..."
  az network nsg delete --resource-group "$AZURE_RESOURCE_GROUP" --name "$test_name"

  # Remove temporary SSH key files
  echo "Removing temporary SSH key files..."
  rm -f "$temp_key_file" "${temp_key_file}.pub"
  echo "Cleanup complete"
}

testKairosImage() {
  local image_id="$1"
  local vm_size="Standard_B1s"
  local test_name
  test_name="kairos-test-$(date +%s)"
  local temp_key_file="/tmp/${test_name}.pem"

  # Generate temporary SSH key pair
  echo "Generating temporary SSH key pair..."
  ssh-keygen -t rsa -b 2048 -f "$temp_key_file" -N "" -q

  # Create a network security group for testing
  echo "Creating network security group..."
  az network nsg create \
    --resource-group "$AZURE_RESOURCE_GROUP" \
    --name "$test_name" \
    --location "$(az group show --name "$AZURE_RESOURCE_GROUP" --query location -o tsv)"

  # Allow SSH access for testing
  az network nsg rule create \
    --resource-group "$AZURE_RESOURCE_GROUP" \
    --nsg-name "$test_name" \
    --name "allow-ssh" \
    --priority 100 \
    --protocol Tcp \
    --destination-port-ranges 22 \
    --access Allow \
    --direction Inbound

  # Create test userdata for Kairos with SSH key
  userdata=$(cat <<EOF
#cloud-config
install:
  auto: true
  reboot: true
  device: auto
  poweroff: false
users:
  - name: kairos
    ssh_authorized_keys:
      - $(cat "${temp_key_file}.pub")
    groups:
    - admin
EOF
)

  echo "Launching test instance..."
  az vm create \
    --resource-group "$AZURE_RESOURCE_GROUP" \
    --name "$test_name" \
    --image "$image_id" \
    --size "$vm_size" \
    --admin-username kairos \
    --ssh-key-values "$(cat "${temp_key_file}.pub")" \
    --nsg "$test_name" \
    --user-data <(echo "$userdata") \
    --public-ip-sku Standard \
    --os-disk-size-gb 40 \
    --security-type Standard

  echo "Test instance $test_name launched"

  # Wait for instance to be running
  waitForInstanceStatus "$test_name" "Succeeded"

  # Get instance public IP
  public_ip=$(az vm show --resource-group "$AZURE_RESOURCE_GROUP" --name "$test_name" \
    --query "networkProfile.networkInterfaces[0].id" --output tsv | \
    xargs -I {} az network nic show --ids {} --query "ipConfigurations[0].publicIPAddress.id" --output tsv | \
    xargs -I {} az network public-ip show --ids {} --query "ipAddress" --output tsv)

  if [ -z "$public_ip" ]; then
    echo "Error: Public IP is empty!"
    exit 1
  fi

  # Set proper permissions on the key file
  chmod 600 "$temp_key_file"

  echo "Testing Kairos installation and boot state..."
  if ! waitForKairosActiveBoot "$public_ip" "$temp_key_file"; then
    echo "Failed to verify Kairos active_boot state"
    cleanupTestResources "$test_name" "$temp_key_file"
    exit 1
  fi

  # Cleanup
  echo "Test successful! Cleaning up resources..."
  cleanupTestResources "$test_name" "$temp_key_file"
}

main() {
  if [ $# -ne 1 ]; then
    echo "Error: You need to specify the Azure image ID to test."
    echo "Usage: $0 <image-id>"
    exit 1
  fi

  checkEnvVars
  
  local image_id="$1"
  testKairosImage "$image_id"
}

# Run main if script is not sourced
if [ "${BASH_SOURCE[0]}" -ef "$0" ]; then
  main "$@"
fi 