# Adds one member to one role and leaves the rest of the policy alone.
resource "marmot_asset_iam_member" "etl_reads_orders" {
  asset_id = marmot_asset.orders.id
  role     = "catalog.viewer"
  member   = "serviceAccount:${marmot_service_account.etl.id}"
}
