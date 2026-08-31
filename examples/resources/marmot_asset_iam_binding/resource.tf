# Owns one role on the asset: members not listed here lose it, other roles are
# left alone.
resource "marmot_asset_iam_binding" "orders_readers" {
  asset_id = marmot_asset.orders.id
  role     = "catalog-reader"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
