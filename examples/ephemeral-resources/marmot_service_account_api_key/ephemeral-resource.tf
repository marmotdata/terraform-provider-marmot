# Mint a key that lives for exactly one Terraform operation: created at open,
# revoked at close, never written to plan or state. Hand it to another
# provider or a write-only attribute.
ephemeral "marmot_service_account_api_key" "ingest_agent" {
  service_account_id = marmot_service_account.ingest_agent.id
}

provider "someprovider" {
  token = ephemeral.marmot_service_account_api_key.ingest_agent.key
}
