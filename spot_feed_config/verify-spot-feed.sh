#!/usr/bin/env bash
set -euo pipefail

AWS_REGION="${AWS_REGION:-}"
SPOT_FEED_BUCKET="${SPOT_FEED_BUCKET:-}"
SPOT_FEED_PREFIX="${SPOT_FEED_PREFIX:-spot-data-feed}"
NAMESPACE="${NAMESPACE:-opencost}"

if [[ -z "$AWS_REGION" ]]; then
  echo "ERROR: AWS_REGION is required" >&2
  exit 1
fi
if [[ -z "$SPOT_FEED_BUCKET" ]]; then
  echo "ERROR: SPOT_FEED_BUCKET is required" >&2
  exit 1
fi

aws ec2 describe-spot-datafeed-subscription --region "$AWS_REGION" | jq -e '.SpotDatafeedSubscription.State == "Active"' >/dev/null
aws s3 ls "s3://${SPOT_FEED_BUCKET}/${SPOT_FEED_PREFIX}/" --recursive | tail -5 >/dev/null || true

POD="$(kubectl -n "$NAMESPACE" get pod -l app.kubernetes.io/name=opencost -o jsonpath='{.items[0].metadata.name}')"
kubectl -n "$NAMESPACE" logs "$POD" -c opencost --tail=500 | rg -n "Spot Pricing Refresh scheduled|Looking up spot data from feed|Found spot info for:" || true
