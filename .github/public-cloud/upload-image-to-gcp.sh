#!/bin/bash

# This script uploads a raw disk image to Google Cloud Storage,
# imports it as a GCE image, makes it public, and replicates it across regions.
# Equivalent to AWS AMI upload script.

set -e
set -o pipefail
set -x

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/cleanup-old-images-gcp.sh"

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
    # Clean up any orphaned image import from a previous failed run.
    # The delete should be synchronous but we verify it's gone before proceeding,
    # because creating a new import while the old one is still deleting can cause
    # the new import job to fail.
    if gcloud migration vms image-imports describe "$name" \
        --location=europe-west3 \
        --project="$GCP_PROJECT" > /dev/null 2>&1; then
      echo "Found existing image import '$name', deleting..."
      gcloud migration vms image-imports delete "$name" \
        --location=europe-west3 \
        --project="$GCP_PROJECT" \
        --quiet
      # Wait until the resource is actually gone
      while gcloud migration vms image-imports describe "$name" \
          --location=europe-west3 \
          --project="$GCP_PROJECT" > /dev/null 2>&1; do
        echo "Waiting for old image import to be fully deleted..."
        sleep 10
      done
      echo "Old image import deleted."
    fi
    gcloud migration vms image-imports create "$name" \
      --image-name="$name" \
      --location=europe-west3 \
      --target-project="$GCP_PROJECT" \
      --source-file="gs://$GCS_BUCKET/$fileName" \
      --family-name="kairos" \
      --skip-os-adaptation \
      --labels="version=$kairosVersion"

    # Poll until the image is ready. The image import is async so we need to
    # check both the image status and the import job state. Without checking
    # the import job, a failed import leaves us polling NOT_FOUND forever.
    local maxRetries=60  # 60 * 30s = 30 minutes
    local retryCount=0
    while true; do
      status=$(gcloud compute images describe "$name" --project="$GCP_PROJECT" --format="value(status)" 2>/dev/null || echo "NOT_FOUND")

      if [[ "$status" == "READY" ]]; then
        echo "Import completed successfully!"
        break
      elif [[ "$status" == "FAILED" ]]; then
        echo "Import failed!"
        exit 1
      elif [[ "$status" == "NOT_FOUND" ]]; then
        # Image doesn't exist yet — check whether the import job is still running
        importState=$(gcloud migration vms image-imports describe "$name" \
          --location=europe-west3 \
          --project="$GCP_PROJECT" \
          --format="value(recentImageImportJobs[0].state)" 2>/dev/null || echo "UNKNOWN")
        if [[ "$importState" == "FAILED" ]]; then
          echo "Image import job failed! Check the import job logs for details."
          exit 1
        fi
        echo "Image not found yet (import state: $importState), waiting..."
      else
        echo "Still in progress... (Current status: $status)"
      fi

      retryCount=$((retryCount + 1))
      if [[ $retryCount -ge $maxRetries ]]; then
        echo "Timed out waiting for image import after $((maxRetries * 30 / 60)) minutes."
        exit 1
      fi

      sleep 30  # Wait before checking again
    done
  fi

  echo "$name"

  # Test the image before making it public
  echo "Testing Kairos image before making it public..."
  if ! "$SCRIPT_DIR/test-gcp-image.sh" "$name"; then
    echo "Image test failed! Not proceeding with making the image public."
    exit 1
  fi
  echo "Image test passed successfully. Proceeding with making image public..."

  # https://cloud.google.com/compute/docs/images/managing-access-custom-images
  # Make the image public
  gcloud compute images add-iam-policy-binding "$name" \
    --member='allAuthenticatedUsers' \
    --role='roles/compute.imageUser'
  echo "Image '$name' is now public."

  # Cleanup: delete the image import after the image is imported.
  # Use || true so a cleanup failure doesn't fail the script after the image
  # is already imported, tested, and made public.
  echo "Cleaning up by deleting the image import process."
  gcloud migration vms image-imports delete "$name" \
    --location=europe-west3 \
    --project="$GCP_PROJECT" \
    --quiet || true
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
