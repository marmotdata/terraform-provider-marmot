# Renders a policy document for the authoritative *_iam_policy resources. It
# makes no API calls.
data "marmot_iam_policy" "orders" {
  binding {
    role = "catalog-reader"
    members = [
      "serviceAccount:${marmot_service_account.etl.id}",
      "group:${marmot_team.analysts.id}",
    ]
  }

  binding {
    role    = "admin"
    members = ["group:${marmot_team.platform.id}"]
  }
}

resource "marmot_asset_iam_policy" "orders" {
  asset_id    = marmot_asset.orders.id
  policy_data = data.marmot_iam_policy.orders.policy_data
}
