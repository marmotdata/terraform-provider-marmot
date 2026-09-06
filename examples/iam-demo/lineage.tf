# How data moves: storefront topics into the lake and the OLTP store, then into
# the warehouse, then into the dashboard and the model.

resource "marmot_lineage" "orders_to_processor" {
  source = marmot_asset.orders_events.mrn
  target = marmot_asset.order_processor.mrn
}

resource "marmot_lineage" "orders_to_lake" {
  source = marmot_asset.orders_events.mrn
  target = marmot_asset.raw_events_lake.mrn
}

resource "marmot_lineage" "payments_to_lake" {
  source = marmot_asset.payments_events.mrn
  target = marmot_asset.raw_events_lake.mrn
}

resource "marmot_lineage" "payments_to_reconciler" {
  source = marmot_asset.payments_events.mrn
  target = marmot_asset.payments_reconciler.mrn
}

resource "marmot_lineage" "processor_to_oltp" {
  source = marmot_asset.order_processor.mrn
  target = marmot_asset.commerce_oltp.mrn
}

resource "marmot_lineage" "processor_to_catalog_api" {
  source = marmot_asset.order_processor.mrn
  target = marmot_asset.catalog_api.mrn
}

resource "marmot_lineage" "lake_to_orders_fact" {
  source = marmot_asset.raw_events_lake.mrn
  target = marmot_asset.orders_fact.mrn
}

resource "marmot_lineage" "oltp_to_customers_dim" {
  source = marmot_asset.commerce_oltp.mrn
  target = marmot_asset.customers_dim.mrn
}

resource "marmot_lineage" "orders_fact_to_revenue" {
  source = marmot_asset.orders_fact.mrn
  target = marmot_asset.revenue_daily.mrn
}

resource "marmot_lineage" "reconciler_to_revenue" {
  source = marmot_asset.payments_reconciler.mrn
  target = marmot_asset.revenue_daily.mrn
}

resource "marmot_lineage" "revenue_to_dashboard" {
  source = marmot_asset.revenue_daily.mrn
  target = marmot_asset.exec_revenue_dashboard.mrn
}

resource "marmot_lineage" "orders_fact_to_churn" {
  source = marmot_asset.orders_fact.mrn
  target = marmot_asset.churn_model.mrn
}

resource "marmot_lineage" "customers_dim_to_churn" {
  source = marmot_asset.customers_dim.mrn
  target = marmot_asset.churn_model.mrn
}
