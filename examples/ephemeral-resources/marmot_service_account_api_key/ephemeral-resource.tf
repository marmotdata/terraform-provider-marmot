ephemeral "marmot_service_account_api_key" "etl" {
  service_account_id = marmot_service_account.etl.id
}

provider "someprovider" {
  token = ephemeral.marmot_service_account_api_key.etl.key
}
