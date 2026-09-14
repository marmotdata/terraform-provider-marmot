resource "marmot_data_product_iam_binding" "orders_admins" {
  data_product_id = marmot_data_product.orders.id
  role            = "admin"
  members         = ["group:${marmot_team.platform.id}"]
}

resource "marmot_data_product_iam_binding" "orders_readers" {
  data_product_id = marmot_data_product.orders.id
  role            = "user"
  members         = ["allAuthenticated"]
}
