resource "marmot_data_product_iam_member" "alice_orders" {
  data_product_id = marmot_data_product.orders.id
  role            = "admin"
  member          = "user:${marmot_user.alice.id}"
}
