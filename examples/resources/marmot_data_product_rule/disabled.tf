resource "marmot_data_product_rule" "legacy_orders" {
  data_product_id = marmot_data_product.orders.id

  name             = "legacy-orders"
  type             = "query"
  query_expression = "tag:legacy-orders"
  enabled          = false
}
