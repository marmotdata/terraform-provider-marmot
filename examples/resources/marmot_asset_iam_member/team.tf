resource "marmot_asset_iam_member" "analysts_orders" {
  asset_id = marmot_asset.orders.id
  role     = "user"
  member   = "group:${marmot_team.analysts.id}"
}
