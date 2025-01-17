#!/bin/bash

# Given a raw image created with Auroraboot, this script will upload it to the speficied AWS account as a public AMI.
# Docs:
# https://docs.aws.amazon.com/vm-import/latest/userguide/required-permissions.html
# https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/creating-an-ami-ebs.html#creating-launching-ami-from-snapshot
# https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/set-ami-boot-mode.html
# https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/launch-instance-boot-mode.html

set -e
set -o pipefail

checkArguments() {
  if [ $# -lt 1 ]; then
    echo "Error: You need to specify the cloud image to upload."
    echo "Usage: $0 <cloud-image>"
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
  if [ -z "$AWS_PROFILE" ] || [ -z "$AWS_REGION" ] || [ -z "$AWS_S3_BUCKET" ]; then
    echo "Error: AWS_PROFILE, AWS_REGION and AWS_S3_BUCKET environment variables must be set."
    exit 1
  fi
}

AWS() {
  aws --profile $AWS_PROFILE --region $AWS_REGION "$@"
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
  local file="$1"
  local baseName=$(basename "$file")
  local s3Path="s3://$AWS_S3_BUCKET/$file"

  if AWS s3 ls "$AWS_S3_BUCKET/$baseName" > /dev/null 2>&1; then
    echo "File '$baseName' already exists in S3 bucket '$AWS_S3_BUCKET'."
  else
    echo "File '$baseName' does not exist in S3 bucket '$AWS_S3_BUCKET'. Uploading now."
    AWS s3 cp $1 s3://$AWS_S3_BUCKET/$baseName
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

  echo $(AWS ec2 describe-import-snapshot-tasks --import-task-ids "$taskID" --query 'ImportSnapshotTasks[0].SnapshotTaskDetail.SnapshotId' --output text)
}

importAsSnapshot() {
  local file="$1"
  local snapshotID

  snapshotID=$(AWS ec2 describe-snapshots --filters "Name=tag:SourceFile,Values=$file" --query "Snapshots[0].SnapshotId" --output text)
  if [ "$snapshotID" != "None" ]; then
    echo "Snapshot $snapshotID already exists for file $file"
    echo $snapshotID
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
  ) --query 'ImportTaskId' --output text)
  if [ $? -ne 0 ]; then
    echo "Failed to import snapshot"
    return 1
  fi

  echo "Snapshot import task started with ID: $taskID"

  snapshotID=$(waitForSnapshotCompletion "$taskID" | tail -1 | tee /dev/tty)
  echo "Adding tag to the snapshot with ID: $snapshotID"
  AWS ec2 create-tags --resources $snapshotID --tags Key=SourceFile,Value=$file

  echo "$snapshotID" # Return the snapshot ID so that we can grab it with `tail -1`
}

checkImageExistsOrCreate() {
  local imageName="$1"
  local snapshotID="$2"
  local imageID

  # Check if the image already exists
  imageID=$(AWS ec2 describe-images --filters "Name=name,Values=$imageName" --query 'Images[0].ImageId' --output text)

  if [ "$imageID" != "None" ]; then
    echo "Image '$imageName' already exists with Image ID: $imageID"
  else
    echo "Image '$imageName' does not exist. Creating from snapshot..."

    imageID=$(AWS ec2 register-image \
      --name "$imageName" \
      --description "AMI created from snapshot $snapshotID" \
      --architecture x86_64 \
      --root-device-name /dev/xvda \
      --block-device-mappings "[{\"DeviceName\":\"/dev/xvda\",\"Ebs\":{\"SnapshotId\":\"$snapshotID\"}}]" \
      --virtualization-type hvm \
      --boot-mode uefi \
      --ena-support \
      --query 'ImageId' \
      --output text)

    echo "Image '$imageName' created with Image ID: $imageID"
  fi
}

makeAMIpublic() {
  local imageName="$1"
  local imageID

  imageID=$(AWS ec2 describe-images --filters "Name=name,Values=$imageName" --query 'Images[0].ImageId' --output text)

  if [ "$imageID" == "None" ]; then
    echo "Error: Image '$imageName' does not exist."
    exit 1
  fi

  echo "Making image '$imageName' public..."
  AWS ec2 modify-image-attribute --image-id $imageID --launch-permission "{\"Add\":[{\"Group\":\"all\"}]}"
  echo "Image '$imageName' is now public."
}

# ----- Main script -----
baseName=$(basename "$1")
checkEnvVars
checkArguments "$@"
ensureVmImportRole
uploadImageToS3 $1
output=$(importAsSnapshot $baseName | tee /dev/tty)
snapshotID=$(echo "$output" | tail -1)
checkImageExistsOrCreate $baseName $snapshotID
makeAMIpublic $baseName
