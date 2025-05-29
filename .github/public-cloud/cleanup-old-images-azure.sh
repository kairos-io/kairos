#!/bin/bash

set -euo pipefail

AZURE_GALLERY_NAME="kairos.io"
AZURE_IMAGE_DEFINITION="kairos"
# Use same variables defined in the main upload script
# AZURE_RESOURCE_GROUP and STORAGE_REGION should already be set in the environment

getAllVersions() {
  az sig image-version list \
    --resource-group "$AZURE_RESOURCE_GROUP" \
    --gallery-name "$AZURE_GALLERY_NAME" \
    --gallery-image-definition "$AZURE_IMAGE_DEFINITION" \
    --query "[].name" \
    --output tsv
}

deleteVersion() {
  local version=$1
  echo "Deleting old image version: $version"
  az sig image-version delete \
    --resource-group "$AZURE_RESOURCE_GROUP" \
    --gallery-name "$AZURE_GALLERY_NAME" \
    --gallery-image-definition "$AZURE_IMAGE_DEFINITION" \
    --gallery-image-version "$version"
}

cleanupOldVersions() {
  echo "Fetching all image versions..."
  mapfile -t allVersions < <(getAllVersions)

  if (( ${#allVersions[@]} <= 4 )); then
    echo "4 or fewer image versions found. No cleanup needed."
    return
  fi

  echo "Sorting versions..."
  mapfile -t sortedVersions < <(printf "%s\n" "${allVersions[@]}" | sort -V -r)

  echo "Keeping latest 4 versions:" "${sortedVersions[@]:0:4}"
  oldVersions=("${sortedVersions[@]:4}")

  # Filter out non-stable versions (those containing '-rc', '-beta', '-alpha', etc.)
  for version in "${oldVersions[@]}"; do
    if [[ ! $version =~ -(rc|beta|alpha|dev|pre|test) ]]; then
      deleteVersion "$version"
    fi
  done
}

cleanupOldVersions
