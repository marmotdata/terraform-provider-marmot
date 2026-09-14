resource "marmot_asset_iam_binding" "orders_admins" {
  asset_id = marmot_asset.orders.id
  role     = "admin"
  members  = ["group:${marmot_team.platform.id}"]
}

resource "marmot_asset_iam_binding" "orders_readers" {
  asset_id = marmot_asset.orders.id
  role     = "user"
  members  = ["allAuthenticated"]
}
