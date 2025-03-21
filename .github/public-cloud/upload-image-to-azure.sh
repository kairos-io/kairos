#!/bin/bash

set -e
set -o pipefail

# https://github.com/Azure/login?tab=readme-ov-file#azure-login-action
export AZURE_CORE_OUTPUT=none

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
  if [ -z "$AZURE_RESOURCE_GROUP" ] || [ -z "$AZURE_STORAGE_ACCOUNT" ] || [ -z "$AZURE_CONTAINER_NAME" ]; then
    echo "Error: AZURE_RESOURCE_GROUP, AZURE_STORAGE_ACCOUNT and AZURE_CONTAINER_NAME environment variables must be set."
    exit 1
  fi
}

LOCAL_VHD_PATH=$(readlink -f "$1")
NAME=$(basename "$LOCAL_VHD_PATH" | sed 's/\.raw\.vhd$//') # just the file name without extension or path
VERSION=$2

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/cleanup-old-images-azure.sh"

checkArguments "$@"
checkEnvVars

# === CHECK AZURE LOGIN ===
az account show >/dev/null 2>&1 || { echo "Please run 'az login' first."; exit 1; }

# === GET STORAGE ACCOUNT REGION ===
echo "Fetching storage account region..."
STORAGE_REGION=$(az storage account show --name "$AZURE_STORAGE_ACCOUNT" --query "primaryLocation" --output tsv)
echo "Storage account is in region: $STORAGE_REGION"

# === GET STORAGE ACCOUNT KEY ===
echo "Fetching storage account key..."
STORAGE_KEY=$(az storage account keys list --account-name "$AZURE_STORAGE_ACCOUNT" --query "[0].value" --output tsv)

echo "Uploading VHD file ($LOCAL_VHD_PATH) to Azure Storage..."
az storage blob upload --account-name "$AZURE_STORAGE_ACCOUNT" --container-name "$AZURE_CONTAINER_NAME" --type page \
    --name "$NAME" --file "$LOCAL_VHD_PATH" --auth-mode key --account-key "$STORAGE_KEY" --overwrite true

echo "Retrieving uploaded VHD URL..."
VHD_URL=$(az storage blob url --account-name "$AZURE_STORAGE_ACCOUNT" --container-name "$AZURE_CONTAINER_NAME" --name "$NAME" --output tsv)
echo "VHD uploaded successfully: $VHD_URL"

# === Get file size in bytes ===
VHD_SIZE_BYTES=$(stat -c %s "$LOCAL_VHD_PATH")

# === Convert to GB and round up ===
VHD_SIZE_GB=$(( (VHD_SIZE_BYTES + 1073741823) / 1073741824 ))
echo "Calculated disk size: $VHD_SIZE_GB GB"

echo "Creating a managed disk"
az disk create \
  --resource-group "$AZURE_RESOURCE_GROUP" \
  --name "$NAME" \
  --source "$VHD_URL" \
  --os-type Linux \
  --sku Premium_LRS \
  --size-gb "$VHD_SIZE_GB" \
  --hyper-v-generation V2

echo "Getting the ID of the managed disk"
DISK_ID=$(az disk show \
  --resource-group "$AZURE_RESOURCE_GROUP" \
  --name "$NAME" \
  --query "id" \
  --output tsv)

# === CREATE AZURE IMAGE IN SAME REGION AS STORAGE ACCOUNT ===
echo "Creating Azure image ($NAME) from VHD in region: $STORAGE_REGION..."
az image create \
	--resource-group "$AZURE_RESOURCE_GROUP" \
	--name "$NAME" \
	--os-type "Linux" \
	--source "$DISK_ID" \
	--hyper-v-generation "V2" \
	--location "$STORAGE_REGION"
echo "Image created successfully: $NAME"

echo "Getting the image ID"
IMAGE_ID=$(az image show \
	--resource-group "$AZURE_RESOURCE_GROUP" \
	--name "$NAME" \
  --query "id" \
  --output tsv)

# echo "Creating a Shared Image Gallery (one-off)"
# # https://learn.microsoft.com/en-us/azure/virtual-machines/create-gallery?tabs=portal%2Cportaldirect%2Ccli2
# # TODO: Link to some EULA?
# az sig create \
#    --gallery-name kairos.io \
#    --permissions community \
#    --resource-group "$AZURE_RESOURCE_GROUP" \
#    --location "$STORAGE_REGION" \
#    --publisher-uri kairos.io \
#    --publisher-email members@kairos.io \
#    --eula https://github.com/kairos-io/kairos/?tab=Apache-2.0-1-ov-file#readme \
#    --public-name-prefix kairos

# echo "Creating an image definition (one-off)"
# az sig image-definition create --resource-group "$AZURE_RESOURCE_GROUP" --gallery-name kairos.io \
#   --gallery-image-definition kairos --publisher kairos.io --offer kairos --sku kairos \
#   --hyper-v-generation "V2" --os-type Linux
#
# echo "Making the gallery public (one-off)"
# az sig share enable-community --resource-group "$AZURE_RESOURCE_GROUP" --gallery-name kairos.io

echo "Creating a Shared image version"
az sig image-version create --resource-group "$AZURE_RESOURCE_GROUP" --gallery-name kairos.io \
  --gallery-image-definition "kairos" --gallery-image-version "${VERSION#v}" \
  --managed-image "$IMAGE_ID" --location "$STORAGE_REGION"

echo "Deleting the managed disk"
az disk delete \
  --resource-group "$AZURE_RESOURCE_GROUP" \
  --name "$NAME" \
  --yes

echo "Deleting managed image (no longer needed)"
az image delete \
  --resource-group "$AZURE_RESOURCE_GROUP" \
  --name "$NAME"

echo "Deleting uploaded VHD blob from Azure Storage..."
az storage blob delete \
  --account-name "$AZURE_STORAGE_ACCOUNT" \
  --container-name "$AZURE_CONTAINER_NAME" \
  --name "$NAME" \
  --auth-mode key \
  --account-key "$STORAGE_KEY"
