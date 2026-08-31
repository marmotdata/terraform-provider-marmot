# A durable key slot. The plaintext is never stored in state; use the
# ephemeral marmot_service_account_api_key to obtain a usable key in a run.
resource "marmot_service_account_api_key" "ingest_agent" {
  service_account_id = marmot_service_account.ingest_agent.id
  name               = "ci-runner"
  expires_in_days    = 90
}
