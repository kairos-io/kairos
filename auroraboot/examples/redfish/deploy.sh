#!/usr/bin/env bash
# examples/redfish/deploy.sh
#
# Two examples of deploying a Kairos ISO to a server via Redfish.
# AuroraBoot uses URL-pull: the BMC fetches the ISO from a URL.
#
# Prerequisites:
#   - auroraboot is in PATH (or replace with: docker run quay.io/kairos/auroraboot)
#   - The BMC Redfish endpoint is reachable from this machine
#   - The ISO is reachable from the BMC (see the two options below)
#
# Set these before running:
BMC_ENDPOINT="https://bmc.example.com"   # Redfish endpoint
BMC_USER="admin"
BMC_PASSWORD_FILE="/run/secrets/bmc-password"  # file holding the password (no trailing newline)

# ─── Option 1: operator-hosted ISO URL ────────────────────────────────────────
#
# The ISO is hosted somewhere the BMC can already reach (HTTP server, S3, etc.).
# Pass the URL directly; AuroraBoot validates it and hands it to InsertMedia.
#
# Usage: ./deploy.sh --image-url

if [[ "${1:-}" == "--image-url" ]]; then
  IMAGE_URL="${2:?Usage: $0 --image-url <url>}"

  auroraboot redfish deploy \
    --endpoint        "$BMC_ENDPOINT" \
    --username        "$BMC_USER" \
    --password-file   "$BMC_PASSWORD_FILE" \
    --image-url       "$IMAGE_URL" \
    --vendor          generic \
    --verify-ssl      \
    --min-memory      4 \
    --min-cpus        2 \
    --required-features UEFI \
    --timeout         30m

  exit $?
fi

# ─── Option 2: local ISO served by AuroraBoot ─────────────────────────────────
#
# Pass a local ISO path and tell AuroraBoot which address the BMC can reach it
# on. AuroraBoot starts a one-shot tokenized HTTP server, registers the ISO
# under a random token, calls InsertMedia with the resulting URL, then shuts
# the server down when the deploy returns.
#
# SERVE_URL is the base URL the BMC will use (must include the IP/hostname that
# the BMC management network can reach). Leave SERVE_ADDR empty to bind the
# host:port of SERVE_URL; set it when the local bind address differs.
#
# Usage: ./deploy.sh <path/to/kairos.iso>

ISO_PATH="${1:?Usage: $0 <path/to/kairos.iso>   OR   $0 --image-url <url>}"
SERVE_URL="http://10.0.0.5:8090"    # adjust to your management-network address
SERVE_ADDR=""                       # set only if the bind address differs from SERVE_URL

SERVE_ADDR_FLAG=()
if [[ -n "$SERVE_ADDR" ]]; then
  SERVE_ADDR_FLAG=(--redfish-serve-addr "$SERVE_ADDR")
fi

auroraboot redfish deploy \
  --endpoint            "$BMC_ENDPOINT" \
  --username            "$BMC_USER" \
  --password-file       "$BMC_PASSWORD_FILE" \
  --redfish-serve-url   "$SERVE_URL" \
  "${SERVE_ADDR_FLAG[@]}" \
  --vendor              generic \
  --verify-ssl          \
  --min-memory          4 \
  --min-cpus            2 \
  --required-features   UEFI \
  --timeout             30m \
  "$ISO_PATH"

# NOTE: Run with Docker by mounting the ISO and the password file:
#
#   docker run --rm \
#     -v "$ISO_PATH:/kairos.iso" \
#     -v "$BMC_PASSWORD_FILE:/run/secrets/bmc-password:ro" \
#     --network host \
#     quay.io/kairos/auroraboot redfish deploy \
#       --endpoint          https://bmc.example.com \
#       --username          admin \
#       --password-file     /run/secrets/bmc-password \
#       --redfish-serve-url http://10.0.0.5:8090 \
#       /kairos.iso
#
# --network host is required so AuroraBoot can bind the serve address on the
# management-network interface.
