resource "marmot_data_product_rule" "commerce_domain" {
  data_product_id = marmot_data_product.orders.id

  name           = "commerce-domain"
  type           = "metadata_match"
  metadata_field = "domain"
  pattern_type   = "exact"
  pattern_value  = "commerce"
}
