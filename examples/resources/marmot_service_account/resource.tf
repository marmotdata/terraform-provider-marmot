resource "marmot_service_account" "ingest_agent" {
  name        = "orders-ingest-agent"
  description = "Ingestion agent for the orders pipeline"
}

# Grant it access per resource; it needs no organization-level role.
resource "marmot_asset_iam_member" "agent_reads_orders" {
  asset_id = marmot_asset.orders.id
  role     = "catalog-reader"
  member   = "serviceAccount:${marmot_service_account.ingest_agent.id}"
}
