resource "marmot_asset_iam_binding" "orders_editors" {
  asset_id = marmot_asset.orders.id
  role     = "editor"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
