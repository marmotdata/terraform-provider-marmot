resource "marmot_data_product_iam_binding" "orders_editors" {
  data_product_id = marmot_data_product.orders.id
  role            = "editor"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
