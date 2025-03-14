#!/bin/bash

set -e
set -o pipefail

# Function to run gcloud commands with the configured project
gcloudCmd() {
  gcloud --project "$GCP_PROJECT" "$@"
}

# Function to get the highest 4 stable versions from GCE images
getHighest4StableVersions() {
  local versions
  local stableVersions=()
  local sortedVersions
  local highest4StableVersions

  # Get all Kairos image versions
  mapfile -t versions < <(gcloudCmd compute images list --filter="family=kairos" --format="value(labels.version)" | sort -u)
  
  # Filter out non-stable versions (those containing '-rc')
  for version in "${versions[@]}"; do
    if [[ ! $version =~ -rc ]]; then
      stableVersions+=("$version")
    fi
  done
  
  # Sort versions and get the highest 4
  IFS=$'\n' mapfile -t sortedVersions < <(printf '%s\n' "${stableVersions[@]}" | sort -V -r)
  unset IFS
  highest4StableVersions=("${sortedVersions[@]:0:4}")

  # Return the highest 4 stable versions
  echo "${highest4StableVersions[@]}"
}

# Function to delete images that are not in the latest 4 stable versions
imageDeleteIfNotInVersionList() {
  local image=$1
  shift 1
  local versionList=("$@")
  
  # Get the image version
  local imageVersion
  imageVersion=$(gcloudCmd compute images describe "$image" --format="value(labels.version)" 2>/dev/null || echo "UNKNOWN")

  # Check if imageVersion is in versionList
  local versionFound="false"
  for version in "${versionList[@]}"; do
    if [[ "$imageVersion" == "$version" ]]; then
      versionFound="true"
      break
    fi
  done

  if [[ "$versionFound" == "true" ]]; then
    echo "Image $image has a stable version ($imageVersion). Skipping cleanup."
  else
    gcloudCmd compute images delete "$image" --quiet
    echo "Deleted image $image as it does not match any of the versions: '${versionList[*]}'"
  fi
}

# Function to clean up old GCS objects
s3ObjectDeleteIfNotInVersionList() {
  local key=$1
  shift 1
  local versionList=("$@")

  # Get object tags (labels in GCS)
  local objectVersion
  objectVersion=$(gcloudCmd storage objects describe "$key" --format="value(custom_fields.version)" 2>/dev/null || echo "UNKNOWN")

  # Check if objectVersion is in versionList
  local versionFound="false"
  for version in "${versionList[@]}"; do
    if [[ "$objectVersion" == "$version" ]]; then
      versionFound="true"
      break
    fi
  done

  if [[ "$versionFound" == "true" ]]; then
    echo "GCS object '$key' has a stable version ($objectVersion). Skipping cleanup."
  else
    gcloudCmd storage rm "$key"
    echo "Deleted GCS object $key as it does not match any of the versions: '${versionList[*]}'"
  fi
}

# Main cleanup function
cleanupOldVersions() {
  if [ -z "$GCP_PROJECT" ] || [ -z "$GCS_BUCKET" ]; then
    echo "Error: GCP_PROJECT and GCS_BUCKET environment variables must be set."
    exit 1
  fi

  read -r -a highest4StableVersions < <(getHighest4StableVersions)
  echo "Highest 4 stable versions: ${highest4StableVersions[*]}"

  # Cleanup images
  mapfile -t allImages < <(gcloudCmd compute images list --filter="family=kairos" --format="value(name)")
  for img in "${allImages[@]}"; do
    imageDeleteIfNotInVersionList "$img" "${highest4StableVersions[@]}"
  done

  # Cleanup GCS objects
  mapfile -t allS3Objects < <(gcloudCmd storage ls "gs://$GCS_BUCKET")
  for s3Object in "${allS3Objects[@]}"; do
    s3ObjectDeleteIfNotInVersionList "$s3Object" "${highest4StableVersions[@]}"
  done
}
