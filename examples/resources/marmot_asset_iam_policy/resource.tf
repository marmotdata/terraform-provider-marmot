# Owns the whole policy. Anything not listed is revoked on the next apply,
# so don't also point an _iam_binding or _iam_member at this asset.
data "marmot_iam_policy" "orders" {
  binding {
    role    = "catalog.viewer"
    members = ["serviceAccount:${marmot_service_account.etl.id}"]
  }
}

resource "marmot_asset_iam_policy" "orders" {
  asset_id    = marmot_asset.orders.id
  policy_data = data.marmot_iam_policy.orders.policy_data
}
