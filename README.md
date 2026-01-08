# AWS OpenCost + Cloud Costs Exporter (Prometheus) + Grafana

This repo is a **starter bundle** to run OpenCost in Kubernetes and enable AWS-related features, including **AWS CUR/Athena cloud costs**, an **OpenCost CloudCost → Prometheus exporter**, and **Grafana dashboards**.

## What to read (entry points)

- **Main Helm values**: [`values.yaml`](./values.yaml)
- **Install/upgrade script**: [`upgrade.sh`](./upgrade.sh)
- **AWS cloud costs (CUR/Athena)**: [`cloud_costs/README.md`](./cloud_costs/README.md)
- **Cloud Costs exporter (Prometheus metrics)**: [`cloud_costs_exporter/README.md`](./cloud_costs_exporter/README.md)
- **Grafana dashboards**: [`grafana_dashboards/README.md`](./grafana_dashboards/README.md)
- **AWS Spot Data Feed**: [`spot_feed_config/README.md`](./spot_feed_config/README.md)
- **Step-by-step guide (optional)**: [`OpenCost and Amazon Cloud Exporter Setup Guide.md`](./OpenCost%20and%20Amazon%20Cloud%20Exporter%20Setup%20Guide.md)  
  Use this when you want a single end-to-end walkthrough; the sub-READMEs above are the detailed references.

## Prerequisites

- Kubernetes access (`kubectl`)
- Helm 3
- A Prometheus-compatible datasource OpenCost can query (Prometheus / VictoriaMetrics Prometheus API)
- For AWS features: IAM permissions + Kubernetes→AWS auth (e.g. IRSA)

## Quick start (OpenCost)

1) Edit [`values.yaml`](./values.yaml) for your cluster (at minimum: `opencost.exporter.defaultClusterId` + `opencost.prometheus.*`).
2) Deploy:

```bash
./upgrade.sh
```

3) Verify:

```bash
kubectl -n opencost get pods
kubectl -n opencost port-forward deploy/opencost 9003:9003
curl -fsS http://127.0.0.1:9003/healthz
```

## Enable AWS cloud costs (CUR/Athena)

Follow: [`cloud_costs/README.md`](./cloud_costs/README.md)

You will configure:
- CUR in S3 + Athena table/workgroup + results bucket
- IAM for OpenCost (commonly IRSA)
- OpenCost `cloud-integration.json` + apply it to the cluster

## Enable Cloud Costs exporter + Grafana dashboard

1) Build/push the exporter image and deploy it: [`cloud_costs_exporter/README.md`](./cloud_costs_exporter/README.md)  
2) Import dashboards into Grafana: [`grafana_dashboards/README.md`](./grafana_dashboards/README.md)

## Optional: AWS Spot Data Feed

Follow: [`spot_feed_config/README.md`](./spot_feed_config/README.md)

## References

- [OpenCost GitHub](https://github.com/opencost/opencost)
- [OpenCost documentation](https://opencost.io/docs/)
- [OpenCost troubleshooting](https://opencost.io/docs/troubleshooting/)
- [Helm chart values reference](https://github.com/opencost/opencost-helm-chart/blob/main/charts/opencost/values.yaml)
- [AWS cloud billing integration background (Kubecost)](https://www.ibm.com/docs/en/kubecost/self-hosted/3.x?topic=integrations-aws-cloud-billing-integration)
