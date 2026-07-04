resource "marmot_team" "analytics" {
  name        = "analytics"
  description = "Owns the reporting datasets"
}

resource "marmot_data_product" "orders" {
  name        = "orders"
  description = "Order events and the tables derived from them"

  tags = ["orders"]

  owner_team_ids = [marmot_team.analytics.id]

  metadata = {
    domain = "commerce"
  }
}

resource "marmot_asset" "orders_table" {
  name     = "orders"
  type     = "dataset"
  services = ["PostgreSQL"]
}

# Add an asset directly.
resource "marmot_data_product_asset" "orders_table" {
  data_product_id = marmot_data_product.orders.id
  asset_id        = marmot_asset.orders_table.id
}

# Or pull assets in by query.
resource "marmot_data_product_rule" "order_datasets" {
  data_product_id = marmot_data_product.orders.id

  name             = "order-datasets"
  type             = "query"
  query_expression = "tag:orders"
}
