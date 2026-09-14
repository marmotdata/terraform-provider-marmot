resource "marmot_data_product" "orders" {
  name = "orders"
}

resource "marmot_data_product_asset" "orders_table" {
  data_product_id = marmot_data_product.orders.id
  asset_id        = marmot_asset.orders.id
}

resource "marmot_data_product_rule" "tagged_orders" {
  data_product_id  = marmot_data_product.orders.id
  name             = "tagged-orders"
  type             = "query"
  query_expression = "tag:orders"
}
