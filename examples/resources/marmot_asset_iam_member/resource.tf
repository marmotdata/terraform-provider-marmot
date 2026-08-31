# Grant one service account read access to one asset, without touching any
# other grant on it. Use this when several configurations manage access to the
# same asset.
resource "marmot_asset_iam_member" "etl_reads_orders" {
  asset_id = marmot_asset.orders.id
  role     = "catalog-reader"
  member   = "serviceAccount:${marmot_service_account.etl.id}"
}
