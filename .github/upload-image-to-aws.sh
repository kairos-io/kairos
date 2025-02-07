#!/bin/bash

# Given a raw image created with Auroraboot, this script will upload it to the speficied AWS account as a public AMI.
# Docs:
# https://docs.aws.amazon.com/vm-import/latest/userguide/required-permissions.html
# https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/creating-an-ami-ebs.html#creating-launching-ami-from-snapshot
# https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/set-ami-boot-mode.html
# https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/launch-instance-boot-mode.html

set -e
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/cleanup-old-images.sh"

checkArguments() {
  if [ $# -lt 2 ]; then
    echo "Error: You need to specify the cloud image to upload and the Kairos version (to tag resources)."
    echo "Usage: $0 <cloud-image> <kairos-version>"
    exit 1
  fi

  local file="$1"

  if [ ! -f "$file" ]; then
    echo "Error: File '$file' does not exist."
    exit 1
  fi

  if ! file "$file" | grep -q 'DOS/MBR boot sector'; then
    echo "Error: File '$file' is not a raw image."
    exit 1
  fi
}

checkEnvVars() {
  if [ -z "$AWS_REGION" ] || [ -z "$AWS_S3_BUCKET" ]; then
    echo "Error: AWS_REGION and AWS_S3_BUCKET environment variables must be set."
    exit 1
  fi
}

AWS() {
  if [ -z "$AWS_PROFILE" ]; then
    aws --region "$AWS_REGION" "$@"
  else
    aws --region "$AWS_REGION" --profile "$AWS_PROFILE" "$@"
  fi
}

# AWS wrapper without passing a region (AWS(N)o(R)egion)
AWSNR() {
  if [ -z "$AWS_PROFILE" ]; then
    aws "$@"
  else
    aws --profile "$AWS_PROFILE" "$@"
  fi
}

# https://docs.aws.amazon.com/vm-import/latest/userguide/required-permissions.html#vmimport-role
ensureVmImportRole() {
  (AWS iam list-roles | jq -r '.Roles[] | select(.RoleName | contains("vmimport")) | .RoleName' | grep -q "vmimport" && echo "vmimport role found. All good.") || {
    echo "Creating vmimport role"

    export AWS_PAGER="" # Avoid being dropped to a pager
    AWS iam create-role --role-name vmimport --assume-role-policy-document file://<(cat <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "vmie.amazonaws.com"
      },
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringEquals": {
          "sts:ExternalId": "vmimport"
        }
      }
    }
  ]
}
EOF
)

  #  AWS iam attach-role-policy --role-name vmimport --policy-arn arn:aws:iam::aws:policy/service-role/AmazonEC2RoleforSSM

    AWS iam put-role-policy --role-name vmimport --policy-name vmimport --policy-document file://<(cat <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetBucketLocation",
        "s3:GetBucketAcl",
        "s3:GetObject",
        "s3:PutObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::$AWS_S3_BUCKET",
        "arn:aws:s3:::$AWS_S3_BUCKET/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "ec2:ModifySnapshotAttribute",
        "ec2:CopySnapshot",
        "ec2:RegisterImage",
        "ec2:Describe*"
      ],
      "Resource": "*"
    }
  ]
}
EOF
)

    sleep 10 # Wait for the policy and permissions to be effective. This is not ideal but I couldn't find any better way.
  }
}

uploadImageToS3() {
  local file
  local baseName
  file="$1"
  kairosVersion="$2"
  baseName=$(basename "$file")

  if AWS s3 ls "$AWS_S3_BUCKET/$baseName" > /dev/null 2>&1; then
    echo "File '$baseName' already exists in S3 bucket '$AWS_S3_BUCKET'."
  else
    echo "File '$baseName' does not exist in S3 bucket '$AWS_S3_BUCKET'. Uploading now."
    AWS s3 cp "$1" "s3://$AWS_S3_BUCKET/$baseName"

    AWS s3api put-object-tagging --bucket "$AWS_S3_BUCKET" --key "$baseName" --tagging "TagSet=[{Key=KairosVersion,Value=$2}]"
  fi
}

waitForSnapshotCompletion() {
  local taskID="$1"
  local status=""

  while true; do
    status=$(AWS ec2 describe-import-snapshot-tasks --import-task-ids "$taskID" --query 'ImportSnapshotTasks[0].SnapshotTaskDetail.Status' --output text)

    if [ "$status" == "completed" ]; then
      echo "Snapshot import completed."
      break
    elif [ "$status" == "deleted" ] || [ "$status" == "cancelling" ] || [ "$status" == "cancelled" ]; then
      echo "Snapshot import failed with status: $status"
      exit 1
    else
      echo "Waiting for snapshot import to complete. Current status: $status" >&2
      sleep 30
    fi
  done

  AWS ec2 describe-import-snapshot-tasks --import-task-ids "$taskID" --query 'ImportSnapshotTasks[0].SnapshotTaskDetail.SnapshotId' --output text
}

importAsSnapshot() {
  local file="$1"
  local kairosVersion="$2"
  local snapshotID

  snapshotID=$(AWS ec2 describe-snapshots --filters "Name=tag:SourceFile,Values=$file" --query "Snapshots[0].SnapshotId" --output text)
  if [ "$snapshotID" != "None" ]; then
    echo "Snapshot $snapshotID already exists for file $file"
    echo "$snapshotID"
    return 0
  fi

  taskID=$(AWS ec2 import-snapshot --description "$file" --disk-container file://<(cat <<EOF
{
  "Description": "$file",
  "Format": "RAW",
  "UserBucket": {
    "S3Bucket": "$AWS_S3_BUCKET",
    "S3Key": "$file"
  }
}
EOF
  ) --query 'ImportTaskId' --output text | tee  /dev/fd/2) || return 1

  echo "Snapshot import task started with ID: $taskID"

  snapshotID=$(waitForSnapshotCompletion "$taskID" | tail -1 | tee /dev/fd/2)
  echo "Adding tag to the snapshot with ID: $snapshotID"
  AWS ec2 create-tags --resources "$snapshotID" \
    --tags Key=Name,Value="${file}" Key=SourceFile,Value="${file}" Key=KairosVersion,Value="${kairosVersion}"

  echo "$snapshotID" # Return the snapshot ID so that we can grab it with `tail -1`
}

checkImageExistsOrCreate() {
  local imageName="$1"
  local snapshotID="$2"
  local kairosVersion="$3"
  local imageID

  # Check if the image already exists
  imageID=$(AWS ec2 describe-images --filters "Name=name,Values=$imageName" --query 'Images[0].ImageId' --output text)

  if [ "$imageID" != "None" ]; then
    echo "Image '$imageName' already exists with Image ID: $imageID"
  else
    echo "Image '$imageName' does not exist. Creating from snapshot..."

    description="AMI created from snapshot $snapshotID"
    imageID=$(AWS ec2 register-image \
      --name "$imageName" \
      --description "$description" \
      --architecture x86_64 \
      --root-device-name /dev/xvda \
      --block-device-mappings "[{\"DeviceName\":\"/dev/xvda\",\"Ebs\":{\"SnapshotId\":\"$snapshotID\"}}]" \
      --virtualization-type hvm \
      --boot-mode uefi \
      --ena-support \
      --query 'ImageId' \
      --output text)

    AWS ec2 create-tags --resources "$imageID" \
      --tags Key=KairosVersion,Value="$kairosVersion" Key=Name,Value="$imageName" Key=Project,Value=Kairos

    echo "Image '$imageName' created with Image ID: $imageID"
  fi

  waitAMI "$imageID" "$AWS_REGION"
  makeAMIpublic "$imageID" "$AWS_REGION"
  copyToAllRegions "$imageID" "$imageName" "$description" "$kairosVersion"
}

# Function to wait for the AMI to become available
waitAMI() {
  local amiID="$1"
  local region="$2"

  echo "[$region] Waiting for AMI $amiID to be available"
  while true; do
    status=$(AWSNR ec2 describe-images --region "$region" --image-ids "$amiID" --query "Images[0].State" --output text 2>/dev/null)
    if [[ "$status" == "available" ]]; then
      echo "[$region] AMI $amiID is now available!"
      break
    elif [[ "$status" == "pending" || "$status" == "null" ]]; then
      sleep 10
    else
      echo "[$region] AMI is in an unexpected state: $status. Exiting."
      exit 1
    fi
  done
}

makeAMIpublic() {
  local imageID="$1"
  local region="$2"

  echo "[$region] calling DisableImageBlockPublicAccess"
  AWSNR --region "$region" ec2 disable-image-block-public-access > /dev/null 2>&1
  echo "[$region] Making image '$imageID' public..."
  AWSNR --region "$region" ec2 modify-image-attribute --image-id "$imageID" --launch-permission "{\"Add\":[{\"Group\":\"all\"}]}"
  echo "[$region] Image '$imageID' is now public."
}

copyToAllRegions() {
  local imageID="$1"
  local imageName="$2"
  local description="$3"
  local kairosVersion="$4"

  echo "Copying AMI '$imageName ($imageID)' to all regions"
  mapfile -t regions < <(AWS ec2 describe-regions | jq -r '.Regions[].RegionName')
  for reg in "${regions[@]}"; do
    # If the current region is the same as the region we are trying to copy, just ignore, the AMI is already there
    if [[ "${AWS_REGION}" == "${reg}" ]]; then
        continue
    fi
    (
      echo "[$reg] Copying AMI '$imageName' to region $reg"
      # Check if the image already exists in this region
      amiCopyID=$(AWSNR --region "$reg" ec2 describe-images --filters "Name=name,Values=$imageName" --query 'Images[0].ImageId' --output text)
      if [ "$amiCopyID" != "None" ]; then
        echo "[$reg] Image '$imageName' already exists with Image ID: $amiCopyID"
      else
        amiCopyID=$(AWS ec2 copy-image \
          --name "${imageName}" \
          --description "${description}" \
          --source-image-id "${imageID}" \
          --source-region "${AWS_REGION}" \
          --region "${reg}" \
          | jq -r '.ImageId'
        )

        echo "[$reg] Tagging Copied AMI ${amiCopyID}"
      fi

      waitAMI "${amiCopyID}" "${reg}"

      snapshotCopyID=$(AWSNR ec2 describe-images \
        --image-ids "$amiCopyID" \
        --region "$reg" \
        --query 'Images[0].BlockDeviceMappings[0].Ebs.SnapshotId' \
        --output text)
      AWSNR --region "$reg" ec2 create-tags \
        --resources "$snapshotCopyID" \
        --tags Key=Name,Value="$imageName" Key=SourceFile,Value="$imageName" Key=KairosVersion,Value="$kairosVersion"

      AWSNR --region "$reg" ec2 create-tags \
        --resources "$amiCopyID" \
        --tags Key=Name,Value="$imageName" Key=Project,Value=Kairos Key=KairosVersion,Value="$kairosVersion"
      makeAMIpublic "$amiCopyID" "$reg"

      echo "[$reg] AMI Copied: $amiCopyID"
    ) &
  done

  wait # Wait for all background jobs to finish
}

# ----- Main script -----
baseName=$(basename "$1")
kairosVersion="$2"
checkEnvVars
checkArguments "$@"

echo
echo "Performing cleanup of old versions"
cleanupOldVersions
echo "Done cleaning up"
echo

# This is an one-off operation and require additional permissions which we don't need to give to CI.
#ensureVmImportRole
uploadImageToS3 "$1" "$kairosVersion"
output=$(importAsSnapshot "$baseName" "$kairosVersion"| tee /dev/fd/2)
snapshotID=$(echo "$output" | tail -1)
checkImageExistsOrCreate "$baseName" "$snapshotID" "$kairosVersion"
