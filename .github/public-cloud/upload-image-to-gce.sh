#!/bin/bash

# This script uploads a raw disk image to Google Cloud Storage,
# imports it as a GCE image, makes it public, and replicates it across regions.
# Equivalent to AWS AMI upload script.

set -e
set -o pipefail
set -x

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/cleanup-old-images-gce.sh"

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
}

checkEnvVars() {
  if [ -z "$GCP_PROJECT" ] || [ -z "$GCS_BUCKET" ]; then
    echo "Error: GCP_PROJECT and GCS_BUCKET environment variables must be set."
    exit 1
  fi
}

gcloudCmd() {
  gcloud --project "$GCP_PROJECT" "$@"
}

uploadImageToGCS() {
  local file="$1"
  local name="$2"
  local version="$3"

  if gsutil ls "gs://$GCS_BUCKET/$name" > /dev/null 2>&1; then
    echo "File '$name' already exists in GCS bucket '$GCS_BUCKET'."
  else
    echo "Uploading '$name' to GCS bucket '$GCS_BUCKET'."
    gsutil -h "x-goog-meta-version:$version" cp "$file" "gs://$GCS_BUCKET/$name"
  fi
}

importGceImage() {
  local fileName="$1"
  local name
  local kairosVersion="$2"
  name="$(sanitizeString "$fileName")"

  if gcloud compute images describe "$name" --project "$GCP_PROJECT" > /dev/null 2>&1; then
    echo "Image '$name' already exists."

    echo "Making sure it has the version label"
    gcloud compute images add-labels "$name" \
      --project "$GCP_PROJECT" \
      --labels="version=$kairosVersion"
  else
    echo "Importing image '$name' from GCS."
    gcloud migration vms image-imports create "$name" \
      --image-name="$name" \
      --location=europe-west3 \
      --target-project="$GCP_PROJECT" \
      --source-file="gs://$GCS_BUCKET/$fileName" \
      --family-name="kairos" \
      --skip-os-adaptation \
      --labels="version=$kairosVersion"

    while true; do
      status=$(gcloud compute images describe "$name" --project="$GCP_PROJECT" --format="value(status)" 2>/dev/null || echo "NOT_FOUND")

      if [[ "$status" == "READY" ]]; then
        echo "Import completed successfully!"
        break
      elif [[ "$status" == "FAILED" ]]; then
        echo "Import failed!"
        exit 1
      elif [[ "$status" == "NOT_FOUND" ]]; then
        echo "Image not found yet, waiting..."
      else
        echo "Still in progress... (Current status: $status)"
      fi

      sleep 30  # Wait before checking again
    done
  fi

  echo "$name"

  # https://cloud.google.com/compute/docs/images/managing-access-custom-images
  # Make the image public
  gcloud compute images add-iam-policy-binding "$name" \
    --member='allAuthenticatedUsers' \
    --role='roles/compute.imageUser'
  echo "Image '$name' is now public."

  # Cleanup: delete the image import after the image is imported
  echo "Cleaning up by deleting the image import process."
  gcloud migration vms image-imports delete "$name" \
    --location=europe-west3 \
    --project="$GCP_PROJECT" \
    --quiet
  echo "Import process for '$name' has been deleted."
}

# Sanitize names by replacing "." with "-" and removing extensions
# Can be used to generate valid resource names or labels from file paths or
# versions.
sanitizeString() {
  local baseName
  baseName=$(basename "$1")
  echo "${baseName%.tar.gz}" | tr '.' '-'
}

# ----- Main script -----
imageFile="$1"
kairosVersion=$(sanitizeString "$2")
checkEnvVars
checkArguments "$@"

echo
echo "Performing cleanup of old versions"
cleanupOldVersions
echo "Done cleaning up"
echo

name=$(sanitizeString "$imageFile")
fileName=$(basename "$imageFile")

# Note: It's likely that we can point --source-file to the local file directly when we
# import the image. This would allow us to skip uploading to the bucket. We do it like this,
# to be able to check the used raw image file in case the image doesn't work for whatever reason.
# Cleanup only keeps 5 files around so it shouldn't cost much.
uploadImageToGCS "$imageFile" "$fileName" "$kairosVersion"
importGceImage "$fileName" "$kairosVersion"
