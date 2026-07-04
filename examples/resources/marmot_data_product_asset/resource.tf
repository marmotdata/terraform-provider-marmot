resource "marmot_data_product" "orders" {
  name = "orders"
}

resource "marmot_asset" "orders_table" {
  name     = "orders"
  type     = "dataset"
  services = ["PostgreSQL"]
}

# Add the asset to the data product.
resource "marmot_data_product_asset" "orders_table" {
  data_product_id = marmot_data_product.orders.id
  asset_id        = marmot_asset.orders_table.id
}
