resource "marmot_data_product" "orders" {
  name = "orders"
}

# Match assets with a search query.
resource "marmot_data_product_rule" "by_query" {
  data_product_id = marmot_data_product.orders.id

  name             = "order-datasets"
  description      = "Datasets tagged orders"
  type             = "query"
  query_expression = "tag:orders"
}

# Match assets on a metadata field.
resource "marmot_data_product_rule" "by_metadata" {
  data_product_id = marmot_data_product.orders.id

  name           = "commerce-domain"
  type           = "metadata_match"
  metadata_field = "domain"
  pattern_type   = "exact"
  pattern_value  = "commerce"
  priority       = 10
}
