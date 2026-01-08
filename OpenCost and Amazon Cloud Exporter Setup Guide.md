# OpenCost and Amazon Cloud Exporter Setup Guide

OpenCost helps you understand Kubernetes cost allocation using metrics from your cluster. When you also connect OpenCost to AWS billing data (CUR + Athena), you can view cloud costs alongside cluster costs. To make cloud cost data easier to chart in Grafana, you can run a small exporter that converts OpenCost CloudCost API responses into Prometheus metrics.

In this guide, you will:

1. Deploy OpenCost with Helm.
1. Set up AWS cloud costs (CUR + Athena) for OpenCost.
1. Run the optional Cloud Costs exporter.
1. Import Grafana dashboards to visualize cost data.

## What you will set up

OpenCost runs in your cluster and exposes an API/UI (and Prometheus metrics) for cost allocation.

AWS cloud costs integration lets OpenCost query Athena to ingest CUR data stored in S3.

Cloud costs exporter (optional) scrapes OpenCost CloudCost API endpoints and exposes Prometheus metrics at `/metrics`.

Grafana dashboards visualize cost data from Prometheus.

## Prerequisites

You will need:

1. A Kubernetes cluster and `kubectl` access.
1. Helm 3.
1. A Prometheus-compatible datasource that OpenCost can query (Prometheus, VictoriaMetrics, Mimir, etc.).
1. An AWS account for cloud costs (optional, for the CUR/Athena integration).

For AWS CUR/Athena integration you will also need:

1. CUR data stored in S3.
1. An Athena database/table that can query your CUR data.
1. An S3 bucket for Athena query results.
1. A Kubernetes-to-AWS auth method for OpenCost (for example, IRSA on EKS).

## Step 1: Deploy OpenCost

From the repository root:

1. Review `values.yaml` and fill in placeholders that apply to your environment:

1. `opencost.exporter.defaultClusterId`
1. `opencost.prometheus.*` (how OpenCost connects to your metrics backend)

1. Install or upgrade OpenCost:

```bash
export NAMESPACE="opencost"
export RELEASE="opencost"
./upgrade.sh
```

1. Port-forward the UI/API:

```bash
kubectl -n "${NAMESPACE}" port-forward deploy/opencost 9090:9090 9003:9003
```

1. Confirm the service is healthy:

```bash
curl -fsS http://127.0.0.1:9003/healthz
```

## Step 2: Set up AWS cloud costs (CUR + Athena)

OpenCost can query Athena to ingest AWS cloud costs from CUR data stored in S3. In this repository, the `cloud_costs/` folder provides helper scripts for:

1. Creating or updating an IAM role/policy that allows OpenCost to query Athena and read S3.
2. Applying a `cloud-integration.json` configuration as a Kubernetes secret.

### 2.1 Create or update IAM permissions for OpenCost

The script `cloud_costs/setup-aws-resources.sh` expects required inputs via environment variables. It will fail fast if any required value is missing.

Example:

```bash
export AWS_ACCOUNT_ID="<AWS_ACCOUNT_ID>"
export AWS_REGION="<AWS_REGION>"
export OIDC_PROVIDER_ARN="arn:aws:iam::<AWS_ACCOUNT_ID>:oidc-provider/<OIDC_PROVIDER_URL>"

export NAMESPACE="opencost"
export SERVICE_ACCOUNT="opencost"
export ROLE_NAME="<ROLE_NAME>"

export CUR_S3_BUCKET="<CUR_S3_BUCKET>"
export ATHENA_RESULTS_BUCKET="<ATHENA_RESULTS_BUCKET>"

./cloud_costs/setup-aws-resources.sh
```

Notes:

`OIDC_PROVIDER_ARN` is required for IRSA-style trust policies.

The script grants Athena and AWS Glue read permissions (for table metadata), plus S3 read/write access needed for Athena query results.

### 2.2 Create the OpenCost `cloud-integration.json`

Create a local file named `cloud-integration.json`:

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

### 2.3 Apply the integration configuration to Kubernetes

This creates or updates a Kubernetes secret and restarts the OpenCost deployment:

```bash
export CLOUD_INTEGRATION_FILE="/path/to/cloud-integration.json"
./cloud_costs/setup-kubernetes.sh
```

### 2.4 Verify cloud costs are enabled

With port-forwarding enabled (see Step 1), check status:

```bash
curl -sS "http://127.0.0.1:9003/cloudCost/status"
```

If the response indicates errors, check OpenCost logs:

```bash
kubectl -n opencost logs -l app.kubernetes.io/name=opencost --container=opencost --tail=300
```

## Step 3: Run the optional Cloud Costs exporter

If you want Grafana panels built directly from Prometheus metrics (instead of querying the OpenCost CloudCost API), you can deploy the exporter in `cloud_costs_exporter/`.

### 3.0 What the exporter does

OpenCost exposes cloud cost data through HTTP endpoints such as `/cloudCost/status` and `/cloudCost/view/*`. This exporter periodically queries those endpoints and converts the responses into Prometheus gauge metrics so you can:

1. Build Grafana panels using PromQL against your Prometheus-compatible backend.
2. Alert on cloud cost integration health (active/valid, last run time, etc.).
3. Break down cloud costs by aggregate dimensions such as service and category.

The exporter does not calculate costs itself. It mirrors what OpenCost returns, on a schedule, into Prometheus metrics.

### 3.1 How scraping works (Go implementation overview)

The exporter runs a background refresh loop. On each refresh it fetches data from OpenCost, clears previous series, and writes fresh values to gauges.

OpenCost endpoints used:

1. `/cloudCost/status` (integration status, last run, next run)
2. `/cloudCost/view/totals` (total cost for a given window and cost metric)
3. `/cloudCost/view/table` (top rows for aggregates such as service/category/item)
4. `/cloudCost/view/graph` (daily time series used for per-day metrics)

Minimal Go excerpt (as implemented in this repository):

```go
cfg := mustConfig()
e := newExporter(cfg)

go func() {
    t := time.NewTicker(cfg.RefreshInterval)
    defer t.Stop()
    for {
        <-t.C
        ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
        _ = e.scrape(ctx)
        cancel()
    }
}()
```

### 3.2 Configuration

The Helm chart passes configuration to the exporter via environment variables:

1. `OPENCOST_URL`: base URL for the OpenCost API (example: `http://opencost.opencost.svc.cluster.local:9003`)
2. `WINDOW`: time window for queries (example: `14d`)
3. `COST_METRIC`: default OpenCost cost metric to scrape (example: `amortizedNetCost`)
4. `COST_METRICS` (optional): comma-separated list of cost metrics to scrape (defaults to `COST_METRIC` if unset)
5. `AGGREGATES` (optional): comma-separated list of aggregates to scrape (defaults to `service,category` if unset)
6. `REFRESH_INTERVAL`: how often to refresh from OpenCost (example: `30m`)
7. `HTTP_TIMEOUT`: timeout for OpenCost HTTP requests (example: `300s`)

### 3.3 Metrics the exporter exposes

The exporter exposes these core metrics (names and labels are based on the Go implementation in `cloud_costs_exporter/src/main.go`):

1. `opencost_cloudcost_exporter_scrape_success`: `1` if the last scrape succeeded, otherwise `0`.
2. `opencost_cloudcost_exporter_scrape_duration_seconds`: seconds spent in the last scrape.
3. `opencost_cloudcost_integration_up{key,provider,source,connection_status}`: `1` when the integration is active and valid.
4. `opencost_cloudcost_integration_run_timestamp{key,provider,which}`: unix seconds for `which=last_run` and `which=next_run`.
5. `opencost_cloudcost_total_cost{window,cost_metric}`: total cloud cost over the window.
6. `opencost_cloudcost_aggregate_cost{aggregate,name,window,cost_metric}`: cost by aggregate (service, category, item, etc.).
7. `opencost_cloudcost_aggregate_kubernetes_percent{aggregate,name,window,cost_metric}`: Kubernetes percent by aggregate.
8. `opencost_cloudcost_daily_total_cost{day,window,cost_metric}`: daily total cloud cost.
9. `opencost_cloudcost_daily_aggregate_cost{aggregate,name,day,window,cost_metric}`: daily cost by aggregate.
10. Convenience metrics for common aggregates:
    1. `opencost_cloudcost_service_cost{service,window,cost_metric}`
    2. `opencost_cloudcost_service_kubernetes_percent{service,window,cost_metric}`
    3. `opencost_cloudcost_daily_service_cost{service,day,window,cost_metric}`
    4. `opencost_cloudcost_category_cost{category,window,cost_metric}`
    5. `opencost_cloudcost_daily_category_cost{category,day,window,cost_metric}`

### 3.1 Build and push the exporter image

From the repository root:

```bash
export REGISTRY="<REGISTRY>"     # e.g. <AWS_ACCOUNT_ID>.dkr.ecr.<AWS_REGION>.amazonaws.com
export IMAGE_REPO="<REPOSITORY>" # e.g. your-org/opencost-cloud-costs-exporter
export TAG="<TAG>"               # e.g. v0.1.0

docker buildx create --name multi --use >/dev/null 2>&1 || docker buildx use multi

docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f cloud_costs_exporter/Dockerfile \
  -t "$REGISTRY/$IMAGE_REPO:$TAG" \
  --push \
  cloud_costs_exporter
```

### 3.2 Deploy the exporter

This repository includes a wrapper in `upgrade.sh`:

```bash
export DEPLOY_CLOUD_COSTS_EXPORTER=true
./upgrade.sh
```

After deployment, confirm the exporter is running and exposing metrics:

```bash
kubectl -n opencost get pods
```

## Step 4: Configure Grafana dashboards

This repository includes dashboards in `grafana_dashboards/`. To use them:

1. In Grafana, import the dashboards JSON from `grafana_dashboards/`.
1. Select your Prometheus (or Prometheus-compatible) datasource.
1. Validate that cost metrics are present:

1. Cluster cost metrics (from OpenCost) should appear once OpenCost is successfully querying your metrics backend.
1. Cloud cost metrics (from the exporter) appear after OpenCost cloud costs are configured and the exporter is scraping successfully.

If you are specifically looking for Spot Data Feed effects in dashboards, see `spot_feed_config/GRAFANA-SPOT-PRICING.md` for PromQL examples.

## Conclusion

With OpenCost deployed and connected to AWS CUR/Athena, you can track Kubernetes cost allocation and cloud billing data in one place. Adding the optional Cloud Costs exporter makes it easier to chart cloud costs in Grafana using standard Prometheus queries. Use these tools to establish a repeatable, observable cost monitoring workflow for your clusters.
