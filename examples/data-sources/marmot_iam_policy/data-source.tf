data "marmot_iam_policy" "orders" {
  binding {
    role    = "editor"
    members = ["serviceAccount:${marmot_service_account.etl.id}"]
  }
}

resource "marmot_asset_iam_policy" "orders" {
  asset_id    = marmot_asset.orders.id
  policy_data = data.marmot_iam_policy.orders.policy_data
}
