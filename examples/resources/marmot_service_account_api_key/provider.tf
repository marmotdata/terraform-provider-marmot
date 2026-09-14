resource "marmot_service_account_api_key" "ci" {
  service_account_id = marmot_service_account.etl.id
  name               = "ci"
  expires_in_days    = 90
}

resource "github_actions_secret" "marmot_api_key" {
  repository      = "orders-pipeline"
  secret_name     = "MARMOT_API_KEY"
  plaintext_value = marmot_service_account_api_key.ci.key
}
