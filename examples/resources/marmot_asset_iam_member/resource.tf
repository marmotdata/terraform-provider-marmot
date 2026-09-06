# Adds one member to one role and leaves every other grant on the asset alone.
# Reach for this when more than one configuration grants access to the same
# asset, because neither will quietly undo the other's work.
resource "marmot_asset_iam_member" "etl_reads_orders" {
  asset_id = marmot_asset.orders.id
  role     = "catalog.viewer"
  member   = "serviceAccount:${marmot_service_account.etl.id}"
}
