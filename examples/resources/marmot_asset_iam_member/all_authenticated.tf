resource "marmot_asset_iam_member" "everyone_orders" {
  asset_id = marmot_asset.orders.id
  role     = "user"
  member   = "allAuthenticated"
}
