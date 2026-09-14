resource "marmot_lineage" "events_to_orders" {
  source = marmot_asset.orders_events.mrn
  target = marmot_asset.orders.mrn
}

resource "marmot_lineage" "orders_to_daily" {
  source = marmot_asset.orders.mrn
  target = marmot_asset.orders_daily.mrn
}

resource "marmot_lineage" "daily_to_dashboard" {
  source = marmot_asset.orders_daily.mrn
  target = marmot_asset.revenue_dashboard.mrn
}
