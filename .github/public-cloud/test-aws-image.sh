#!/bin/bash

set -e
set -o pipefail

# AWS wrapper with region
AWS() {
  if [ -z "$AWS_PROFILE" ]; then
    aws --region "$AWS_REGION" "$@"
  else
    aws --region "$AWS_REGION" --profile "$AWS_PROFILE" "$@"
  fi
}

checkEnvVars() {
  if [ -z "$AWS_REGION" ]; then
    echo "Error: AWS_REGION environment variable must be set."
    exit 1
  fi
}

waitForInstanceStatus() {
  local instance_id="$1"
  local target_state="$2"
  local status=""

  echo "Waiting for instance $instance_id to reach state $target_state..."
  while true; do
    status=$(AWS ec2 describe-instances --instance-ids "$instance_id" \
      --query 'Reservations[0].Instances[0].State.Name' --output text)

    if [ "$status" == "$target_state" ]; then
      echo "Instance reached state: $target_state"
      break
    elif [ "$status" == "terminated" ] || [ "$status" == "shutting-down" ]; then
      if [ "$target_state" == "terminated" ] && [ "$status" == "shutting-down" ]; then
        echo "Instance is shutting down, waiting for full termination..."
        sleep 10
        continue
      elif [ "$status" != "$target_state" ]; then
        echo "Instance reached terminal state '$status' while waiting for '$target_state'"
        exit 1
      fi
    else
      echo "Current instance state: $status"
      sleep 10
    fi
  done
}

waitForSystemStatus() {
  local instance_id="$1"
  local status=""

  echo "Waiting for instance system status checks..."
  while true; do
    status=$(AWS ec2 describe-instance-status --instance-ids "$instance_id" \
      --query 'InstanceStatuses[0].SystemStatus.Status' --output text)

    if [ "$status" == "ok" ]; then
      echo "System status checks passed"
      break
    elif [ "$status" == "impaired" ]; then
      echo "Instance system status failed - status: $status"
      exit 1
    else
      echo "Current system status: $status"
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

testKairosImage() {
  local ami_id="$1"
  local instance_type="t3.small"
  local test_name
  test_name="kairos-test-$(date +%s)"
  local temp_key_file="/tmp/${test_name}.pem"

  # Generate temporary SSH key pair
  echo "Generating temporary SSH key pair..."
  ssh-keygen -t rsa -b 2048 -f "$temp_key_file" -N "" -q
  
  # Create a security group for testing
  echo "Creating security group..."
  sg_id=$(AWS ec2 create-security-group \
    --group-name "$test_name" \
    --description "Temporary security group for Kairos testing" \
    --query 'GroupId' --output text)
  
  # Allow SSH access for testing
  AWS ec2 authorize-security-group-ingress \
    --group-id "$sg_id" \
    --protocol tcp \
    --port 22 \
    --cidr 0.0.0.0/0

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
  instance_id=$(AWS ec2 run-instances \
    --image-id "$ami_id" \
    --instance-type "$instance_type" \
    --security-group-ids "$sg_id" \
    --user-data "$userdata" \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$test_name}]" \
    --block-device-mappings "[{\"DeviceName\":\"/dev/xvda\",\"Ebs\":{\"VolumeSize\":40}}]" \
    --query 'Instances[0].InstanceId' \
    --output text)

  echo "Test instance $instance_id launched"

  # Wait for instance to be running
  waitForInstanceStatus "$instance_id" "running"
  
  # Wait for system status checks to pass
  waitForSystemStatus "$instance_id"

  # Get instance public IP
  public_ip=$(AWS ec2 describe-instances --instance-ids "$instance_id" \
    --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)

  echo "Testing Kairos installation and boot state..."
  if ! waitForKairosActiveBoot "$public_ip" "$temp_key_file"; then
    echo "Failed to verify Kairos active_boot state"
    AWS ec2 terminate-instances --instance-ids "$instance_id"
    AWS ec2 delete-security-group --group-id "$sg_id"
    rm -f "$temp_key_file" "${temp_key_file}.pub"
    exit 1
  fi

  # Cleanup
  echo "Test successful! Cleaning up resources..."
  AWS ec2 terminate-instances --instance-ids "$instance_id"
  
  # Wait for instance termination
  waitForInstanceStatus "$instance_id" "terminated"
  
  # Delete security group
  AWS ec2 delete-security-group --group-id "$sg_id"
  
  # Remove temporary SSH key
  rm -f "$temp_key_file" "${temp_key_file}.pub"

  echo "Cleanup complete"
}

main() {
  if [ $# -ne 1 ]; then
    echo "Error: You need to specify the AMI ID to test."
    echo "Usage: $0 <ami-id>"
    exit 1
  fi

  checkEnvVars
  
  local ami_id="$1"
  testKairosImage "$ami_id"
}

# Run main if script is not sourced
if [ "${BASH_SOURCE[0]}" -ef "$0" ]; then
  main "$@"
fi 