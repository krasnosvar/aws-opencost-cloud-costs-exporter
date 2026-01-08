#!/usr/bin/env bash
set -euo pipefail

# This script configures an IAM role for OpenCost to query Athena and read CUR data in S3.
# It does not create CUR/Athena resources; those are prerequisites.

AWS_ACCOUNT_ID="${AWS_ACCOUNT_ID:-}"
OIDC_PROVIDER_ARN="${OIDC_PROVIDER_ARN:-}" # arn:aws:iam::<ACCOUNT_ID>:oidc-provider/<OIDC_PROVIDER_URL>
AWS_REGION="${AWS_REGION:-}"
NAMESPACE="${NAMESPACE:-opencost}"
SERVICE_ACCOUNT="${SERVICE_ACCOUNT:-opencost}"

CUR_S3_BUCKET="${CUR_S3_BUCKET:-}"               # bucket with CUR data
ATHENA_RESULTS_BUCKET="${ATHENA_RESULTS_BUCKET:-}" # bucket for Athena query results
SPOT_FEED_BUCKET="${SPOT_FEED_BUCKET:-}"         # optional, for spot feed

ROLE_NAME="${ROLE_NAME:-${SERVICE_ACCOUNT}.${NAMESPACE}.sa}"
POLICY_NAME="${POLICY_NAME:-${ROLE_NAME}-policy}"

require() { [[ -n "${!1}" ]] || { echo "ERROR: $1 is required" >&2; exit 1; }; }
require AWS_ACCOUNT_ID
require OIDC_PROVIDER_ARN
require AWS_REGION
require CUR_S3_BUCKET
require ATHENA_RESULTS_BUCKET

OIDC_PROVIDER_URL="${OIDC_PROVIDER_ARN#*/oidc-provider/}"
SUBJECT="system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

cat > "${tmpdir}/trust-policy.json" <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": { "Federated": "${OIDC_PROVIDER_ARN}" },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "${OIDC_PROVIDER_URL}:sub": "${SUBJECT}"
        }
      }
    }
  ]
}
EOF

cat > "${tmpdir}/iam-policy.json" <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "Athena",
      "Effect": "Allow",
      "Action": [
        "athena:BatchGetQueryExecution",
        "athena:GetQueryExecution",
        "athena:GetQueryResults",
        "athena:GetQueryResultsStream",
        "athena:StartQueryExecution",
        "athena:StopQueryExecution",
        "athena:ListQueryExecutions",
        "athena:GetWorkGroup",
        "athena:ListWorkGroups"
      ],
      "Resource": "*"
    },
    {
      "Sid": "Glue",
      "Effect": "Allow",
      "Action": [
        "glue:GetDatabase",
        "glue:GetDatabases",
        "glue:GetTable",
        "glue:GetTables",
        "glue:GetPartition",
        "glue:GetPartitions"
      ],
      "Resource": "*"
    },
    {
      "Sid": "S3CUR",
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:ListBucket"],
      "Resource": [
        "arn:aws:s3:::${CUR_S3_BUCKET}",
        "arn:aws:s3:::${CUR_S3_BUCKET}/*"
      ]
    },
    {
      "Sid": "S3AthenaResults",
      "Effect": "Allow",
      "Action": [
        "s3:GetBucketLocation",
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListBucket",
        "s3:ListBucketMultipartUploads",
        "s3:ListMultipartUploadParts",
        "s3:AbortMultipartUpload"
      ],
      "Resource": [
        "arn:aws:s3:::${ATHENA_RESULTS_BUCKET}",
        "arn:aws:s3:::${ATHENA_RESULTS_BUCKET}/*"
      ]
    }
  ]
}
EOF

if [[ -n "$SPOT_FEED_BUCKET" ]]; then
  jq ".Statement += [{\"Sid\":\"S3SpotFeed\",\"Effect\":\"Allow\",\"Action\":[\"s3:ListBucket\",\"s3:GetObject\"],\"Resource\":[\"arn:aws:s3:::${SPOT_FEED_BUCKET}\",\"arn:aws:s3:::${SPOT_FEED_BUCKET}/*\"]}]" \
    "${tmpdir}/iam-policy.json" > "${tmpdir}/iam-policy.with-spot.json"
  mv "${tmpdir}/iam-policy.with-spot.json" "${tmpdir}/iam-policy.json"
fi

if aws iam get-role --role-name "${ROLE_NAME}" >/dev/null 2>&1; then
  aws iam update-assume-role-policy --role-name "${ROLE_NAME}" --policy-document "file://${tmpdir}/trust-policy.json"
else
  aws iam create-role --role-name "${ROLE_NAME}" --assume-role-policy-document "file://${tmpdir}/trust-policy.json"
fi

aws iam put-role-policy --role-name "${ROLE_NAME}" --policy-name "${POLICY_NAME}" --policy-document "file://${tmpdir}/iam-policy.json"
QUERY_STATEMENT="SELECT bill_bill_type, bill_billing_entity, bill_billing_period_end_date, bill_billing_period_start_date, bill_invoice_id, bill_invoicing_entity, bill_payer_account_id, bill_payer_account_name, cost_category, discount, discount_bundled_discount, discount_total_discount, identity_line_item_id, identity_time_interval, line_item_availability_zone, line_item_blended_cost, line_item_blended_rate, line_item_currency_code, line_item_legal_entity, line_item_line_item_description, line_item_line_item_type, line_item_net_unblended_cost, line_item_net_unblended_rate, line_item_normalization_factor, line_item_normalized_usage_amount, line_item_operation, line_item_product_code, line_item_resource_id, line_item_tax_type, line_item_unblended_cost, line_item_unblended_rate, line_item_usage_account_id, line_item_usage_account_name, line_item_usage_amount, line_item_usage_end_date, line_item_usage_start_date, line_item_usage_type, pricing_currency, pricing_lease_contract_length, pricing_offering_class, pricing_public_on_demand_cost, pricing_public_on_demand_rate, pricing_purchase_option, pricing_rate_code, pricing_rate_id, pricing_term, pricing_unit, product, product_comment, product_fee_code, product_fee_description, product_from_location, product_from_location_type, product_from_region_code, product_instance_family, product_instance_type, product_instancesku, product_location, product_location_type, product_operation, product_pricing_unit, product_product_family, product_region_code, product_servicecode, product_sku, product_to_location, product_to_location_type, product_to_region_code, product_usagetype, reservation_amortized_upfront_cost_for_usage, reservation_amortized_upfront_fee_for_billing_period, reservation_availability_zone, reservation_effective_cost, reservation_end_time, reservation_modification_status, reservation_net_amortized_upfront_cost_for_usage, reservation_net_amortized_upfront_fee_for_billing_period, reservation_net_effective_cost, reservation_net_recurring_fee_for_usage, reservation_net_unused_amortized_upfront_fee_for_billing_period, reservation_net_unused_recurring_fee, reservation_net_upfront_value, reservation_normalized_units_per_reservation, reservation_number_of_reservations, reservation_recurring_fee_for_usage, reservation_reservation_a_r_n, reservation_start_time, reservation_subscription_id, reservation_total_reserved_normalized_units, reservation_total_reserved_units, reservation_units_per_reservation, reservation_unused_amortized_upfront_fee_for_billing_period, reservation_unused_normalized_unit_quantity, reservation_unused_quantity, reservation_unused_recurring_fee, reservation_upfront_value, resource_tags, savings_plan_amortized_upfront_commitment_for_billing_period, savings_plan_end_time, savings_plan_instance_type_family, savings_plan_net_amortized_upfront_commitment_for_billing_period, savings_plan_net_recurring_commitment_for_billing_period, savings_plan_net_savings_plan_effective_cost, savings_plan_offering_type, savings_plan_payment_option, savings_plan_purchase_term, savings_plan_recurring_commitment_for_billing_period, savings_plan_region, savings_plan_savings_plan_a_r_n, savings_plan_savings_plan_effective_cost, savings_plan_savings_plan_rate, savings_plan_start_time, savings_plan_total_commitment_to_date, savings_plan_used_commitment, split_line_item_actual_usage, split_line_item_net_split_cost, split_line_item_net_unused_cost, split_line_item_parent_resource_id, split_line_item_public_on_demand_split_cost, split_line_item_public_on_demand_unused_cost, split_line_item_reserved_usage, split_line_item_split_cost, split_line_item_split_usage, split_line_item_split_usage_ratio, split_line_item_unused_cost FROM COST_AND_USAGE_REPORT"

cat > /tmp/data-query.json <<EOF
{
  "QueryStatement": "$QUERY_STATEMENT",
  "TableConfigurations": {
    "COST_AND_USAGE_REPORT": {
      "INCLUDE_MANUAL_DISCOUNT_COMPATIBILITY": "FALSE",
      "INCLUDE_RESOURCES": "TRUE",
      "INCLUDE_SPLIT_COST_ALLOCATION_DATA": "TRUE",
      "TIME_GRANULARITY": "HOURLY"
    }
  }
}
EOF

cat > /tmp/create-export.json <<EOF
{
  "Export": {
    "Name": "$EXPORT_NAME",
    "DataQuery": $(cat /tmp/data-query.json),
    "DestinationConfigurations": {
      "S3Destination": {
        "S3Bucket": "$BUCKET_NAME",
        "S3Prefix": "$S3_PREFIX",
        "S3Region": "$REGION",
        "S3OutputConfigurations": {
          "Format": "PARQUET",
          "Compression": "PARQUET",
          "OutputType": "CUSTOM",
          "Overwrite": "OVERWRITE_REPORT"
        }
      }
    },
    "RefreshCadence": {"Frequency": "SYNCHRONOUS"}
  }
}
EOF

EXISTING_ARN=$(aws bcm-data-exports list-exports --region "$EXPORT_REGION" --output json 2>&1 | jq -r ".Exports[] | select(.Name == \"$EXPORT_NAME\") | .ExportArn" || echo "")
[ -n "$EXISTING_ARN" ] && aws bcm-data-exports delete-export --export-arn "$EXISTING_ARN" --region "$EXPORT_REGION" 2>&1 || true

aws bcm-data-exports create-export --cli-input-json file:///tmp/create-export.json --region "$EXPORT_REGION"

# Step 12: Annotate Kubernetes service account
kubectl annotate serviceaccount "${SERVICE_ACCOUNT}" \
    -n "${NAMESPACE}" \
    eks.amazonaws.com/role-arn="arn:aws:iam::${ACCOUNT_ID}:role/${ROLE_NAME}" \
    --overwrite
