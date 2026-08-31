# Own the asset's entire policy. Any binding not described here is removed, so
# do not combine this with marmot_asset_iam_binding or marmot_asset_iam_member
# on the same asset — they will overwrite each other on every apply.
data "marmot_iam_policy" "orders" {
  binding {
    role    = "catalog-reader"
    members = ["serviceAccount:${marmot_service_account.etl.id}"]
  }
}

resource "marmot_asset_iam_policy" "orders" {
  asset_id    = marmot_asset.orders.id
  policy_data = data.marmot_iam_policy.orders.policy_data
}
