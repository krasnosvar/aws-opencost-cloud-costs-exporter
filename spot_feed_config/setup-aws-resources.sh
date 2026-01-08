#!/usr/bin/env bash
set -euo pipefail

# Required:
# - SPOT_FEED_BUCKET: S3 bucket name for Spot Data Feed
# - AWS_REGION: region for bucket/subscription
#
# Optional:
# - SPOT_FEED_PREFIX (default: spot-data-feed)
SPOT_FEED_BUCKET="${SPOT_FEED_BUCKET:-}"
AWS_REGION="${AWS_REGION:-}"
SPOT_FEED_PREFIX="${SPOT_FEED_PREFIX:-spot-data-feed}"

if [[ -z "$SPOT_FEED_BUCKET" ]]; then
  echo "ERROR: SPOT_FEED_BUCKET is required" >&2
  exit 1
fi
if [[ -z "$AWS_REGION" ]]; then
  echo "ERROR: AWS_REGION is required" >&2
  exit 1
fi

# Step 1: Create S3 bucket
aws s3api create-bucket --bucket "$SPOT_FEED_BUCKET" --region "$AWS_REGION" --create-bucket-configuration LocationConstraint="$AWS_REGION" 2>&1 | rg -q "Location" || true

# Step 2: Set bucket ownership controls
aws s3api put-bucket-ownership-controls --bucket "$SPOT_FEED_BUCKET" --ownership-controls '{"Rules": [{"ObjectOwnership": "BucketOwnerPreferred"}]}' --region "$AWS_REGION"

# Step 3: Set public access block settings
aws s3api put-public-access-block --bucket "$SPOT_FEED_BUCKET" --public-access-block-configuration "BlockPublicAcls=false,IgnorePublicAcls=false,BlockPublicPolicy=true,RestrictPublicBuckets=true" --region "$AWS_REGION"

# Step 3b: Set lifecycle policy to expire objects older than 90 days
aws s3api put-bucket-lifecycle-configuration --bucket "$SPOT_FEED_BUCKET" --lifecycle-configuration '{
  "Rules": [
    {
      "ID": "expire-spot-feed-after-90d",
      "Status": "Enabled",
      "Expiration": { "Days": 90 },
      "Filter": { "Prefix": "'"$SPOT_FEED_PREFIX"'/"
      }
    }
  ]
}' --region "$AWS_REGION"

# Step 4: Delete existing Spot Data Feed subscription if exists
aws ec2 delete-spot-datafeed-subscription --region "$AWS_REGION" 2>&1 | rg -q "SpotDatafeedSubscription" || true

# Step 5: Create Spot Data Feed subscription
aws ec2 create-spot-datafeed-subscription --bucket "$SPOT_FEED_BUCKET" --prefix "$SPOT_FEED_PREFIX" --region "$AWS_REGION"
