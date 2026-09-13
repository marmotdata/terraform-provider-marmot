# A new service account reaches nothing until it is granted something.
resource "marmot_service_account" "ingest_agent" {
  name        = "orders-ingest-agent"
  description = "Ingestion agent for the orders pipeline"
}

# Grant it the one asset it needs.
resource "marmot_asset_iam_member" "agent_reads_orders" {
  asset_id = marmot_asset.orders.id
  role     = "catalog.viewer"
  member   = "serviceAccount:${marmot_service_account.ingest_agent.id}"
}
