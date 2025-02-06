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

  # TODO:
  # Make the script stop when things fail (it didn't?). Like so:
  # $(dosomething | tail -1 | tee /dev/fd/2)

  # get all image tags
  mapfile -t imgTags < <(AWSNR --region $reg ec2 describe-images --image-ids $img --query 'Images[].Tags[]' --output text)
  TagExists=false
  for tag in "${imgTags[@]}"; do
    for tagToCheck in "${versionList[@]}"; do
      if [[ $tag == "KairosVersion"*"$tagToCheck" ]]; then
        echo "[$reg] AMI $img has the '$tagToCheck' tag. Skipping cleanup."
        TagExists=true
        break 2
      fi
    done
  done

  if [ "$TagExists" = false ]; then
      AWSNR --region $reg ec2 deregister-image --image-id $img
      echo "[$reg] AMI $img deleted because it does not match any of the versions: '${versionList[@]}'."
  fi
}

snapshotDeleteIfNotInVersionList() {
  local reg=$1
  local snapshot=$2
  shift 2
  local versionList=("$@")

  # Get all snapshot tags
  mapfile -t snapshotTags < <(AWSNR --region $reg ec2 describe-snapshots --snapshot-ids $snapshot --query 'Snapshots[].Tags[]' --output text)
  TagExists=false
  for tag in "${snapshotTags[@]}"; do
    for tagToCheck in "${versionList[@]}"; do
      if [[ $tag == "KairosVersion"*"$tagToCheck" ]]; then
        echo "[$reg] Snapshot $snapshot has the '$tagToCheck' tag. Skipping cleanup."
        TagExists=true
        break
      fi
    done
  done

  if [ "$TagExists" = false ]; then
    (AWSNR --region $reg ec2 delete-snapshot --snapshot-id $snapshot && \
      echo "[$reg] Snapshot $snapshot deleted because it does not match any of the versions: '${versionList[@]}'.") || true
  fi
}

s3ObjectDeleteIfNotInVersionList() {
  local bucket=$1
  local key=$2
  shift 2
  local versionList=("$@")

  # Get all S3 object tags
  mapfile -t s3Tags < <(AWSNR s3api get-object-tagging --bucket "$bucket" --key "$key" --query 'TagSet[]' --output text)

  TagExists=false
  for tag in "${s3Tags[@]}"; do
    for tagToCheck in "${versionList[@]}"; do
      if [[ $tag == "KairosVersion"*"$tagToCheck" ]]; then
        echo "S3 object '$key' in bucket '$bucket' has the '$tagToCheck' tag. Skipping cleanup."
        TagExists=true
        break 2
      fi
    done
  done

  if [ "$TagExists" = false ]; then
    AWSNR s3api delete-object --bucket "$bucket" --key "$key"
    echo "S3 object $key in bucket $bucket deleted because it does not match any of the versions: '${versionList[@]}'."
  fi
}

getHighest4StableVersions() {
  local reg=$1
  local kairosVersions
  local stableVersions=()
  local sortedVersions
  local highest4StableVersions

  # Get all Kairos versions
  mapfile -t kairosVersions < <(AWSNR --region "$reg" ec2 describe-images --owners self --query 'Images[].Tags[?Key==`KairosVersion`].Value' --output text)

  # Filter out non-stable versions (those containing '-rc')
  for version in "${kairosVersions[@]}"; do
    if [[ ! $version =~ -rc ]]; then
      stableVersions+=("$version")
    fi
  done

  # Sort the stable versions and keep only the highest 4
  IFS=$'\n' sortedVersions=($(sort -V -r <<<"${stableVersions[*]}"))
  unset IFS
  highest4StableVersions=("${sortedVersions[@]:0:4}")

  # Return the highest 4 stable versions
  echo "${highest4StableVersions[@]}"
}

cleanupOldVersionsRegion() {
  local reg=$1
  shift 1
  local versionList=("$@")

  # Cleanup AMIs
  mapfile -t allAmis < <(AWSNR --region $reg ec2 describe-images --owners self --query 'Images[].ImageId' --output text | tr '\t' '\n')
  for img in "${allAmis[@]}"; do
    amiDeleteIfNotInVersionList $reg $img "${versionList[@]}"
  done

  # Cleanup Snapshots
  mapfile -t allSnapshots < <(AWSNR --region $reg ec2 describe-snapshots --owner-ids self --query 'Snapshots[].SnapshotId' --output text | tr '\t' '\n')
  for snapshot in "${allSnapshots[@]}"; do
    snapshotDeleteIfNotInVersionList $reg $snapshot "${versionList[@]}"
  done
}

cleanupOldVersions() {
  highest4StableVersions=($(getHighest4StableVersions "$AWS_REGION"))
  echo "Highest 4 stable versions (in region $AWS_REGION): ${highest4StableVersions[@]}"

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
