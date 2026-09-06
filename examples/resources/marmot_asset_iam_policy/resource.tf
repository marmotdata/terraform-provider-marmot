# This resource is the sole owner of the asset's policy, so anything not listed
# below is revoked on the next apply. For the same reason, never point an
# _iam_binding or _iam_member at an asset you manage this way: the two will
# spend every apply undoing each other.
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
