# Creates a long-lived key. The secret itself is never written to state, so if
# you need the usable key during a run, use the ephemeral resource of the same
# name instead.
resource "marmot_service_account_api_key" "ingest_agent" {
  service_account_id = marmot_service_account.ingest_agent.id
  name               = "ci-runner"
  expires_in_days    = 90
}
