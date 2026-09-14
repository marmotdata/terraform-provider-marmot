ephemeral "google_secret_manager_secret_version" "marmot_api_key" {
  secret  = "marmot-api-key"
  version = "latest"
}

provider "marmot" {
  host    = "https://acme.marmotdata.cloud"
  api_key = ephemeral.google_secret_manager_secret_version.marmot_api_key.secret_data
}
