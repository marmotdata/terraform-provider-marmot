resource "marmot_data_product_asset" "orders_table" {
  data_product_id = marmot_data_product.orders.id
  asset_id        = marmot_asset.orders.id
}
