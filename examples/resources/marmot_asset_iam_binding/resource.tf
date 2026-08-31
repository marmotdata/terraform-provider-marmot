# Own one role on the asset. Members not listed here are removed from that role,
# while other roles on the same asset are left alone.
resource "marmot_asset_iam_binding" "orders_readers" {
  asset_id = marmot_asset.orders.id
  role     = "catalog-reader"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
