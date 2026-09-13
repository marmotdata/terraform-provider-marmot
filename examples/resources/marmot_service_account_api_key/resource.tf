# A long-lived key. The plaintext is sensitive and kept in state.
resource "marmot_service_account_api_key" "ingest_agent" {
  service_account_id = marmot_service_account.ingest_agent.id
  name               = "ci-runner"
  expires_in_days    = 90
}
