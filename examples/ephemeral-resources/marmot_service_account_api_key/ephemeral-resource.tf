# Mints a key that lives for exactly one Terraform operation: created when the
# run opens, revoked when it closes, and never written to plan or state. Pass it
# straight to another provider or into a write-only attribute.
ephemeral "marmot_service_account_api_key" "ingest_agent" {
  service_account_id = marmot_service_account.ingest_agent.id
}

provider "someprovider" {
  token = ephemeral.marmot_service_account_api_key.ingest_agent.key
}
