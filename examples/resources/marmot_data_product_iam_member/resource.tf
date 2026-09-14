resource "marmot_data_product_iam_member" "etl_orders" {
  data_product_id = marmot_data_product.orders.id
  role            = "editor"
  member          = "serviceAccount:${marmot_service_account.etl.id}"
}
