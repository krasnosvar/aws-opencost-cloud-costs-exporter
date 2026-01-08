# Spot data feed (AWS)

Spot data feed provides instance-level spot pricing data in S3. OpenCost can use it to improve spot price accuracy.

## Prerequisites

- AWS CLI access
- An IAM role used by OpenCost that can read the spot feed bucket (`s3:GetObject`, `s3:ListBucket`)

## Create/Update the spot feed bucket and subscription

```bash
export AWS_REGION="<AWS_REGION>"
export SPOT_FEED_BUCKET="<SPOT_FEED_BUCKET>"
export SPOT_FEED_PREFIX="spot-data-feed"

./setup-aws-resources.sh
```

## Configure OpenCost (Helm `values.yaml`)

Set these fields in `opencost.customPricing.costModel`:

```yaml
awsSpotDataRegion: "<AWS_REGION>"
awsSpotDataBucket: "<SPOT_FEED_BUCKET>"
awsSpotDataPrefix: "spot-data-feed"
```

## Verify

```bash
export AWS_REGION="<AWS_REGION>"
export SPOT_FEED_BUCKET="<SPOT_FEED_BUCKET>"
./verify-spot-feed.sh
```

## Multi-cluster notes

Keep each cluster's OpenCost installation independent. Reuse shared cloud data sources (CUR/Athena) when appropriate.

## Spot feed (AWS)

- Spot pricing is region-specific.
- Use separate Spot Data Feed buckets per region (or per cluster) and grant each cluster access to its bucket.
- Recommended bucket naming:
  - `opencost-spot-data-feed-<account-id>-<region>-<cluster-id>`

## Cloud costs (AWS CUR/Athena)

- CUR/Athena are account-wide.
- Run OpenCost per cluster; each instance can query the same Athena database/table.
- Attribution depends on tags/provider IDs.
