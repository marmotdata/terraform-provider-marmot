# Grants one role to one member and leaves every other grant on the asset
# alone. Use this when more than one configuration manages the same asset.
resource "marmot_asset_iam_member" "etl_reads_orders" {
  asset_id = marmot_asset.orders.id
  role     = "catalog-reader"
  member   = "serviceAccount:${marmot_service_account.etl.id}"
}
