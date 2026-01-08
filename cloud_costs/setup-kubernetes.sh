#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-opencost}"
SECRET_NAME="${SECRET_NAME:-cloud-costs}"
HELM_RELEASE="${HELM_RELEASE:-opencost}"
HELM_REPO="${HELM_REPO:-https://opencost.github.io/opencost-helm-chart}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VALUES_FILE="${VALUES_FILE:-${SCRIPT_DIR}/../values.yaml}"
CLOUD_INTEGRATION_FILE="${CLOUD_INTEGRATION_FILE:-}"

if [[ -z "$CLOUD_INTEGRATION_FILE" ]]; then
  echo "ERROR: CLOUD_INTEGRATION_FILE is required (path to cloud-integration.json)" >&2
  exit 1
fi

kubectl create secret generic "$SECRET_NAME" \
  --from-file=cloud-integration.json="$CLOUD_INTEGRATION_FILE" \
  -n "$NAMESPACE" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

helm upgrade --install "$HELM_RELEASE" opencost \
  --repo "$HELM_REPO" \
  --namespace "$NAMESPACE" \
  --create-namespace \
  -f "$VALUES_FILE"

kubectl rollout restart deployment "$HELM_RELEASE" -n "$NAMESPACE"
