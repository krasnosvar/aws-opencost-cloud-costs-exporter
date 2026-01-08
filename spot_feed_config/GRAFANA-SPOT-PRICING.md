# Finding Spot Pricing in Grafana

## How OpenCost Exposes Spot Pricing

OpenCost uses spot pricing from the Spot Data Feed when calculating node costs. Spot prices are included in the standard cost metrics; they are not exposed as separate metrics.

### Key Metrics

1. `kubecost_node_is_spot`: indicates whether a node is spot (1) or on-demand (0).
   - Common labels: `instance`, `node`, `instance_type`, `region`, `provider_id`, `arch`, `uid`

2. `node_cpu_hourly_cost`: CPU cost per hour (includes spot pricing for spot nodes).
   - Common labels: `instance`, `node`, `instance_type`, `region`, `provider_id`, `arch`, `uid`

3. `node_ram_hourly_cost`: RAM cost per hour per GB (includes spot pricing for spot nodes).
   - Common labels: `instance`, `node`, `instance_type`, `region`, `provider_id`, `arch`, `uid`

4. `node_total_hourly_cost`: total node cost per hour (includes spot pricing for spot nodes).
   - Common labels: `instance`, `node`, `instance_type`, `region`, `provider_id`, `arch`, `uid`

## How Spot Pricing Works

1. OpenCost reads actual spot prices from the Spot Data Feed (S3 bucket)
2. For each spot node, it uses the price from the feed for that instance type
3. These prices are automatically included in `node_cpu_hourly_cost` and `node_ram_hourly_cost`
4. If feed data is unavailable, it falls back to `spotCPU`/`spotRAM` values from Helm config

## Finding Spot Pricing in Grafana

### Method 1: Filter by Spot Nodes

Add a filter to existing queries to show only spot nodes:

```promql
# Spot nodes only - CPU cost
sum(
  node_cpu_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 1
) by (node, instance_type)

# Spot nodes only - RAM cost
sum(
  node_ram_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 1
) by (node, instance_type)

# Spot nodes only - Total cost
sum(
  node_total_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 1
) by (node, instance_type)
```

### Method 2: Compare Spot vs On-Demand

Create a query that compares spot and on-demand costs:

```promql
# Spot nodes total cost
sum(
  node_total_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 1
) * 730

# On-demand nodes total cost
sum(
  node_total_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 0
) * 730
```

### Method 3: Spot Pricing by Instance Type

See which instance types are using spot pricing:

```promql
# Spot nodes grouped by instance type
sum(
  node_total_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 1
) by (instance_type) * 730
```

### Method 4: Spot vs On-Demand Cost Comparison Table

Create a table showing spot vs on-demand costs for the same instance types:

```promql
# Spot cost per instance type
sum(
  node_total_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 1
) by (instance_type) * 730

# On-demand cost per instance type (for comparison)
sum(
  node_total_hourly_cost{job=~"$job"} * on(node) kubecost_node_is_spot{job=~"$job"} == 0
) by (instance_type) * 730
```

## Example: Spot Node Cost Panel

Add this to your Grafana dashboard to see spot node costs:

Panel title: `Spot nodes monthly cost`

Query:
```promql
sum(
  node_total_hourly_cost{job=~"$job"} 
  * on(node) group_left() 
  kubecost_node_is_spot{job=~"$job"} == 1
) by (node, instance_type) * 730
```

Visualization: table

Columns: Node, Instance Type, Monthly Cost

## Verifying Spot Pricing is Being Used

1. Check whether nodes are detected as spot:
   ```promql
   kubecost_node_is_spot{job=~"$job"}
   ```
   Should return `1` for spot nodes, `0` for on-demand.

2. Compare costs:
   - Spot nodes should have lower `node_cpu_hourly_cost` and `node_ram_hourly_cost` than on-demand nodes of the same instance type
   - Check the actual values in the metrics to see spot pricing in effect

3. Check OpenCost logs:
   ```bash
   kubectl logs -n opencost -l app.kubernetes.io/name=opencost --container=opencost | grep -i "spot"
   ```
   Look for:
   - `Found spot info for: i-xxx` - Spot pricing found
   - `Looking up spot data from feed` - Reading from feed
   - `marked preemptible but we have no data in spot feed` - Using fallback values

## Current Dashboard Integration

The existing dashboard (`22208_rev2.json`) already shows spot pricing because:
- It uses `node_cpu_hourly_cost` and `node_ram_hourly_cost` which include spot pricing
- The "Cost by Instance Type" panel shows costs that already reflect spot pricing
- The "Nodes Monthly Cost" table shows costs per node, which includes spot pricing for spot nodes

To see spot-specific costs, add filters using `kubecost_node_is_spot == 1` to any query.
