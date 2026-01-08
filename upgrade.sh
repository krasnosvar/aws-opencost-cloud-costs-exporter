#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-opencost}"
RELEASE="${RELEASE:-opencost}"
VALUES_FILE="${VALUES_FILE:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/values.yaml}"

helm upgrade --install "$RELEASE" opencost \
  --repo https://opencost.github.io/opencost-helm-chart \
  --namespace "$NAMESPACE" \
  --create-namespace \
  --reset-values \
  -f "$VALUES_FILE"

# Optional: deploy the Cloud Costs exporter (see cloud_costs_exporter/README.md)
if [[ "${DEPLOY_CLOUD_COSTS_EXPORTER:-}" == "true" ]]; then
  helm upgrade --install opencost-cloud-costs-exporter "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/cloud_costs_exporter/chart" \
    --namespace "$NAMESPACE" \
    --create-namespace \
    --reset-values
fi
