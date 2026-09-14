resource "marmot_data_product_rule" "orders_tables" {
  data_product_id = marmot_data_product.orders.id

  name           = "orders-tables"
  type           = "metadata_match"
  metadata_field = "table"
  pattern_type   = "wildcard"
  pattern_value  = "orders_*"
  priority       = 10
}
