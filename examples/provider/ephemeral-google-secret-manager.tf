# Read the API key from Google Secret Manager as an ephemeral value: it never
# lands in Terraform state or plan.
ephemeral "google_secret_manager_secret_version" "marmot_api_key" {
  secret  = "marmot-api-key" # secret name or full resource ID
  version = "latest"
}

provider "marmot" {
  host    = "https://your-marmot-host.com"
  api_key = ephemeral.google_secret_manager_secret_version.marmot_api_key.secret_data
}
