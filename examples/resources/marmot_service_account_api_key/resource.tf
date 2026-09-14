resource "marmot_service_account_api_key" "ci" {
  service_account_id = marmot_service_account.etl.id
  name               = "ci"
  expires_in_days    = 90
}
