# Owns one role on the asset. Anyone left out loses it on the next apply;
# other roles are untouched.
resource "marmot_asset_iam_binding" "orders_readers" {
  asset_id = marmot_asset.orders.id
  role     = "catalog.viewer"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
