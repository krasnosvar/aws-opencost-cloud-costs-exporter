# OpenCost Cloud Costs Exporter

This exporter scrapes OpenCost CloudCost API endpoints and exposes them as Prometheus metrics at `/metrics`.

## How it works

The exporter queries OpenCost on a schedule and converts API responses into Prometheus gauge metrics.

It fetches data from these OpenCost endpoints:

1. `/cloudCost/status` (integration status, last run, next run)
2. `/cloudCost/view/totals` (total cost over a window)
3. `/cloudCost/view/table` (top rows for aggregates such as service, category, and item)
4. `/cloudCost/view/graph` (daily time series used to build per-day metrics)

Scrape behavior:

1. On each refresh, the exporter clears previously exported series and repopulates them from the latest OpenCost responses.
2. If a scrape fails, `opencost_cloudcost_exporter_scrape_success` is set to `0` and the error is logged.

## Configuration

The Helm chart passes these environment variables to the exporter:

1. `OPENCOST_URL` (required): base URL for OpenCost (example: `http://opencost.opencost.svc.cluster.local:9003`)
2. `WINDOW` (required): query window (example: `14d`)
3. `COST_METRIC` (required): default cost metric (example: `amortizedNetCost`)
4. `COST_METRICS` (optional): comma-separated list of cost metrics to scrape (defaults to `COST_METRIC` if unset)
5. `AGGREGATES` (optional): comma-separated list of aggregates to scrape (defaults to `service,category` if unset)
6. `REFRESH_INTERVAL` (optional): refresh interval (defaults to `5m` if unset)
7. `HTTP_TIMEOUT` (optional): request timeout (defaults to `30s` if unset)

## Build and push a multi-arch image (amd64 and arm64)

Run from this repo root:

```bash
export REGISTRY="<REGISTRY>"                 # e.g. <AWS_ACCOUNT_ID>.dkr.ecr.<AWS_REGION>.amazonaws.com
export IMAGE_REPO="<REPOSITORY>"             # e.g. your-org/opencost-cloud-costs-exporter
export TAG="<TAG>"                           # e.g. v0.1.0

# Login to your registry (example for ECR; adjust for your registry):
# aws ecr get-login-password --region "<AWS_REGION>" | docker login --username AWS --password-stdin "$REGISTRY"

# One-time builder
docker buildx create --name multi --use >/dev/null 2>&1 || docker buildx use multi

docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f cloud_costs_exporter/Dockerfile \
  -t "$REGISTRY/$IMAGE_REPO:$TAG" \
  --push \
  cloud_costs_exporter

docker buildx imagetools inspect "$REGISTRY/$IMAGE_REPO:$TAG"
```

## PromQL examples

```promql
# total over window
opencost_cloudcost_total_cost{window="14d",cost_metric="amortizedNetCost"}

# daily totals (daily samples use explicit per-day timestamps; use a range query or last_over_time())
opencost_cloudcost_daily_total_cost{window="14d",cost_metric="amortizedNetCost"}

# top services for a given cost metric
topk(10, opencost_cloudcost_aggregate_cost{aggregate="service",window="14d",cost_metric="netCost"})

# a specific service per day
opencost_cloudcost_daily_aggregate_cost{aggregate="service",name="AmazonEC2",window="14d",cost_metric="amortizedNetCost"}

# "resource-like" breakdown (OpenCost 'item' mode; name includes providerID/category/service)
topk(20, opencost_cloudcost_aggregate_cost{aggregate="item",window="14d",cost_metric="amortizedNetCost"})
```

## Deploy/upgrade via Helm

```bash
helm upgrade opencost-cloud-costs-exporter cloud_costs_exporter/chart \
  --install \
  --namespace opencost \
  --create-namespace
```

## Enable more cost types + resource breakdown

Override values (example):

```yaml
opencost:
  costMetrics:
    - amortizedNetCost
    - netCost
    - listCost
    - amortizedCost
    - invoicedCost
  aggregates:
    - service
    - category
    - item
```
