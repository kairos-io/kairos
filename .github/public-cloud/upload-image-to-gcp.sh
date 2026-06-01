#!/bin/bash

# This script uploads a raw disk image to Google Cloud Storage,
# creates a GCE image from it, makes it public, and replicates it across regions.
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
    echo "Creating image '$name' from the raw disk tarball in GCS."
    # Create the image directly from the gzipped 'disk.raw' tarball in GCS using
    # the native Compute Engine image-creation path. It is synchronous (returns a
    # non-zero exit code on failure) and creates the image in a single step, so
    # there is no async import job to poll or clean up.
    #
    # We deliberately do NOT use 'gcloud migration vms image-imports' (Migrate to
    # Virtual Machines): that service fails on our hadron-based images with an
    # opaque "Internal migration service error" (gRPC INTERNAL) during its
    # creatingImage step. We never wanted OS adaptation anyway (the old code
    # passed --skip-os-adaptation), so the native path is both more reliable and
    # far simpler.
    gcloud compute images create "$name" \
      --project="$GCP_PROJECT" \
      --source-uri="gs://$GCS_BUCKET/$fileName" \
      --family="kairos" \
      --labels="version=$kairosVersion"
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
