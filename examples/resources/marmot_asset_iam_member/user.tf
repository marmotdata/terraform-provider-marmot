resource "marmot_asset_iam_member" "alice_orders" {
  asset_id = marmot_asset.orders.id
  role     = "admin"
  member   = "user:${marmot_user.alice.id}"
}
