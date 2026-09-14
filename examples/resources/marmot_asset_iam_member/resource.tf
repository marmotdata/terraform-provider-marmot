resource "marmot_asset_iam_member" "etl_orders" {
  asset_id = marmot_asset.orders.id
  role     = "editor"
  member   = "serviceAccount:${marmot_service_account.etl.id}"
}
