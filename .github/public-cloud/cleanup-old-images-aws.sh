#!/bin/bash

set -e
set -o pipefail
#set -x

# AWS wrapper without passing a region (AWS(N)o(R)egion)
# Redefined here so that we can source this file alone if needed.
# The other copy is in cleanup-old-images.sh script.
AWSNR() {
  if [ -z "$AWS_PROFILE" ]; then
    aws "$@"
  else
    aws --profile "$AWS_PROFILE" "$@"
  fi
}

amiDeleteIfNotInVersionList() {
  local reg=$1
  local img=$2
  shift 2
  local versionList=("$@")
  echo "DEBUG: Number of versions in amiDeleteIfNotInVersionList: ${#versionList[@]}"
  echo "DEBUG: Versions in amiDeleteIfNotInVersionList: ${versionList[*]}"
  for i in "${!versionList[@]}"; do
    echo "DEBUG: Version $i: ${versionList[$i]}"
  done

  # Get all image tags and properly parse them
  TagExists=false
  echo "DEBUG: Checking AMI $img in region $reg"
  echo "DEBUG: Version list to check against: ${versionList[*]}"

  while IFS=$'\t' read -r key value; do
    # Trim whitespace from both key and value
    key=$(echo "$key" | xargs)
    value=$(echo "$value" | xargs)
    echo "DEBUG: Processing tag - Key: '$key', Value: '$value'"

    if [[ "$key" == "KairosVersion" ]]; then
      for tagToCheck in "${versionList[@]}"; do
        # Trim whitespace from tagToCheck as well
        tagToCheck=$(echo "$tagToCheck" | xargs)
        echo "DEBUG: Comparing value '$value' with tagToCheck '$tagToCheck'"
        if [[ "$value" == "$tagToCheck" ]]; then
          echo "[$reg] AMI $img has the '$tagToCheck' tag. Skipping cleanup."
          TagExists=true
          break 2
        fi
      done
    fi
  done < <(AWSNR --region "$reg" ec2 describe-images --image-ids "$img" --query 'Images[].Tags[].[Key,Value]' --output text)

  if [ "$TagExists" = false ]; then
      AWSNR --region "$reg" ec2 deregister-image --image-id "$img"
      echo "[$reg] AMI $img deleted because it does not match any of the versions: '${versionList[*]}'."
  fi
}

snapshotDeleteIfNotInVersionList() {
  local reg=$1
  local snapshot=$2
  shift 2
  local versionList=("$@")

  # Get all snapshot tags and properly parse them
  TagExists=false
  echo "DEBUG: Checking Snapshot $snapshot in region $reg"
  echo "DEBUG: Version list to check against: ${versionList[*]}"

  while IFS=$'\t' read -r key value; do
    # Trim whitespace from both key and value
    key=$(echo "$key" | xargs)
    value=$(echo "$value" | xargs)
    echo "DEBUG: Processing tag - Key: '$key', Value: '$value'"

    if [[ "$key" == "KairosVersion" ]]; then
      for tagToCheck in "${versionList[@]}"; do
        # Trim whitespace from tagToCheck as well
        tagToCheck=$(echo "$tagToCheck" | xargs)
        echo "DEBUG: Comparing value '$value' with tagToCheck '$tagToCheck'"
        if [[ "$value" == "$tagToCheck" ]]; then
          echo "[$reg] Snapshot $snapshot has the '$tagToCheck' tag. Skipping cleanup."
          TagExists=true
          break 2
        fi
      done
    fi
  done < <(AWSNR --region "$reg" ec2 describe-snapshots --snapshot-ids "$snapshot" --query 'Snapshots[].Tags[].[Key,Value]' --output text)

  if [ "$TagExists" = false ]; then
    (AWSNR --region "$reg" ec2 delete-snapshot --snapshot-id "$snapshot" && \
      echo "[$reg] Snapshot $snapshot deleted because it does not match any of the versions: '${versionList[*]}'.") || true
  fi
}

s3ObjectDeleteIfNotInVersionList() {
  local bucket=$1
  local objectKey=$2
  shift 2
  local versionList=("$@")

  # Get all S3 object tags and properly parse them
  TagExists=false
  echo "DEBUG: Checking S3 object '$objectKey' in bucket '$bucket'"
  echo "DEBUG: Version list to check against: ${versionList[*]}"

  # Check if the object has any tags first
  if AWSNR s3api get-object-tagging --bucket "$bucket" --key "$objectKey" --query 'length(TagSet)' --output text 2>/dev/null | grep -q '^[0-9]'; then
    while IFS=$'\t' read -r tagKey tagValue; do
      # Trim whitespace from both key and value
      tagKey=$(echo "$tagKey" | xargs)
      tagValue=$(echo "$tagValue" | xargs)
      echo "DEBUG: Processing tag - Key: '$tagKey', Value: '$tagValue'"

      if [[ "$tagKey" == "KairosVersion" ]]; then
        for tagToCheck in "${versionList[@]}"; do
          # Trim whitespace from tagToCheck as well
          tagToCheck=$(echo "$tagToCheck" | xargs)
          echo "DEBUG: Comparing value '$tagValue' with tagToCheck '$tagToCheck'"
          if [[ "$tagValue" == "$tagToCheck" ]]; then
            echo "S3 object '$objectKey' in bucket '$bucket' has the '$tagToCheck' tag. Skipping cleanup."
            TagExists=true
            break 2
          fi
        done
      fi
    done < <(AWSNR s3api get-object-tagging --bucket "$bucket" --key "$objectKey" --query 'TagSet[].[Key,Value]' --output text)
  fi

  if [ "$TagExists" = false ]; then
    AWSNR s3api delete-object --bucket "$bucket" --key "$objectKey"
    echo "S3 object $objectKey in bucket $bucket deleted because it does not match any of the versions: '${versionList[*]}'."
  fi
}

getHighest4StableVersions() {
  local reg=$1
  local kairosVersions
  local stableVersions=()
  local sortedVersions
  local highest4StableVersions

  # Get all Kairos versions - AWS CLI already outputs one per line
  readarray -t kairosVersions < <(AWSNR --region "$reg" ec2 describe-images --owners self --query "Images[].Tags[?Key=='KairosVersion'].Value" --output text)

  # Filter out non-stable versions (those containing '-rc', '-beta', '-alpha', etc.)
  for version in "${kairosVersions[@]}"; do
    if [[ ! $version =~ -(rc|beta|alpha|dev|pre|test) ]]; then
      stableVersions+=("$version")
    fi
  done

  # Sort the stable versions and keep only the highest 4
  mapfile -t sortedVersions < <(printf '%s\n' "${stableVersions[@]}" | sort -V -r)
  highest4StableVersions=("${sortedVersions[@]:0:4}")

  # Print each version on a new line
  printf '%s\n' "${highest4StableVersions[@]}"
}

cleanupOldVersionsRegion() {
  local reg=$1
  shift 1
  local versionList=("$@")
  echo "DEBUG: Number of versions in versionList: ${#versionList[@]}"
  echo "DEBUG: Versions in versionList: ${versionList[*]}"
  for i in "${!versionList[@]}"; do
    echo "DEBUG: Version $i: ${versionList[$i]}"
  done

  # Cleanup AMIs
  mapfile -t allAmis < <(AWSNR --region "$reg" ec2 describe-images --owners self --query 'Images[].ImageId' --output text | tr '\t' '\n')
  for img in "${allAmis[@]}"; do
    amiDeleteIfNotInVersionList "$reg" "$img" "${versionList[@]}"
  done

  # Cleanup Snapshots
  mapfile -t allSnapshots < <(AWSNR --region "$reg" ec2 describe-snapshots --owner-ids self --query 'Snapshots[].SnapshotId' --output text | tr '\t' '\n')
  for snapshot in "${allSnapshots[@]}"; do
    snapshotDeleteIfNotInVersionList "$reg" "$snapshot" "${versionList[@]}"
  done
}

cleanupOldVersions() {
  mapfile -t highest4StableVersions < <(getHighest4StableVersions "$AWS_REGION")
  echo "DEBUG: Number of versions in highest4StableVersions: ${#highest4StableVersions[@]}"
  echo "DEBUG: Versions in highest4StableVersions: ${highest4StableVersions[*]}"
  for i in "${!highest4StableVersions[@]}"; do
    echo "DEBUG: Version $i: ${highest4StableVersions[$i]}"
  done

  mapfile -t regions < <(AWSNR ec2 describe-regions | jq -r '.Regions[].RegionName')
  for reg in "${regions[@]}"; do
    cleanupOldVersionsRegion "$reg" "${highest4StableVersions[@]}"
  done

  # Cleanup S3 Objects
  mapfile -t allS3Objects < <(AWSNR s3api list-objects-v2 --bucket "$AWS_S3_BUCKET" --query 'Contents[].Key' --output text| tr '\t' '\n')
  for s3Object in "${allS3Objects[@]}"; do
    s3ObjectDeleteIfNotInVersionList "$AWS_S3_BUCKET" "$s3Object" "${highest4StableVersions[@]}"
  done
}
