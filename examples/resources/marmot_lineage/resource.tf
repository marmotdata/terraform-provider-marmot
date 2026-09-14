resource "marmot_asset" "orders" {
  name     = "orders"
  type     = "Table"
  services = ["PostgreSQL"]
}

resource "marmot_asset" "orders_daily" {
  name     = "orders_daily"
  type     = "Table"
  services = ["BigQuery"]
}

resource "marmot_lineage" "orders_to_daily" {
  source = marmot_asset.orders.mrn
  target = marmot_asset.orders_daily.mrn
}
