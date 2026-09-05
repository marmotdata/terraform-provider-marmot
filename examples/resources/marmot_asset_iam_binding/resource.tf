# This resource decides who holds one particular role on the asset. Anyone left
# out of the list loses that role on the next apply, but roles it does not
# mention are untouched, so another team can keep managing its own.
resource "marmot_asset_iam_binding" "orders_readers" {
  asset_id = marmot_asset.orders.id
  role     = "catalog.viewer"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
