resource "marmot_data_product_iam_member" "analysts_orders" {
  data_product_id = marmot_data_product.orders.id
  role            = "user"
  member          = "group:${marmot_team.analysts.id}"
}
