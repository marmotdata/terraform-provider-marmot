resource "marmot_data_product_rule" "tagged_orders" {
  data_product_id = marmot_data_product.orders.id

  name             = "tagged-orders"
  description      = "Every asset tagged orders"
  type             = "query"
  query_expression = "tag:orders"
}
