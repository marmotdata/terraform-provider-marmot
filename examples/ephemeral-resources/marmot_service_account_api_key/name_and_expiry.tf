ephemeral "marmot_service_account_api_key" "etl" {
  service_account_id = marmot_service_account.etl.id
  name               = "terraform-apply"
  expires_in_days    = 7
}
