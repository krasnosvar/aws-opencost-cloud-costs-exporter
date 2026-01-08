# Cloud costs (AWS CUR/Athena)

OpenCost can ingest AWS cloud costs by querying Athena over CUR data in S3.

## Prerequisites

- AWS CUR available in S3 and queryable in Athena (database/table/workgroup already exist)
- An Athena results bucket (S3) for query output
- A Kubernetes ServiceAccount used by OpenCost with permission to run Athena queries and read the CUR data (typically via IRSA)

## Configure OpenCost

1) Create a `cloud-integration.json` file (example):

```json
{
  "aws": {
    "athena": [
      {
        "bucket": "s3://<ATHENA_RESULTS_BUCKET>/",
        "region": "<AWS_REGION>",
        "database": "<ATHENA_DATABASE>",
        "table": "<ATHENA_TABLE>",
        "workgroup": "<ATHENA_WORKGROUP>",
        "account": "<AWS_ACCOUNT_ID>",
        "authorizer": { "authorizerType": "AWSServiceAccount" }
      }
    ]
  }
}
```

2) Apply it to the cluster and upgrade OpenCost:

```bash
export CLOUD_INTEGRATION_FILE="/path/to/cloud-integration.json"
./setup-kubernetes.sh
```

3) Ensure `values.yaml` sets:

- `opencost.cloudIntegrationSecret` to the secret name
- `opencost.exporter.extraEnv.AWS_ATHENA_BUCKET` to a full `s3://.../` URI
- `opencost.customPricing.costModel.*` Athena fields (used by some features)

## Verify

```bash
kubectl -n opencost logs -l app.kubernetes.io/name=opencost --container=opencost | rg -n "CloudCost|AthenaIntegration|GetCloudCost" || true
kubectl -n opencost port-forward deploy/opencost 19003:9003
curl -sS "http://127.0.0.1:19003/cloudCost/status" | jq .
```

## Athena partitions (monthly)

The underlying Athena table is usually partitioned by billing period (e.g. `billing_period=YYYY-MM`). If the new month partition is missing in Glue/Athena, OpenCost can appear to “stop” at the last day of the last known partition.

Recommended: run a Glue crawler monthly on the 1st day (Glue cron is UTC).

```bash
export AWS_REGION="<AWS_REGION>"
export CRAWLER_NAME="<GLUE_CRAWLER_NAME>"

aws glue update-crawler \\
  --name "$CRAWLER_NAME" \\
  --region "$AWS_REGION" \\
  --schedule 'cron(30 0 1 * ? *)'
```

One-time manual fix example (adjust placeholders):

```sql
ALTER TABLE <ATHENA_DATABASE>.<ATHENA_TABLE>
ADD IF NOT EXISTS PARTITION (billing_period='YYYY-MM')
LOCATION 's3://<CUR_BUCKET>/<PREFIX>/data/BILLING_PERIOD=YYYY-MM/';
```
