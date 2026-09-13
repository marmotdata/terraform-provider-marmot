# Created on open, revoked on close, never in plan or state.
ephemeral "marmot_service_account_api_key" "ingest_agent" {
  service_account_id = marmot_service_account.ingest_agent.id
}

provider "someprovider" {
  token = ephemeral.marmot_service_account_api_key.ingest_agent.key
}
