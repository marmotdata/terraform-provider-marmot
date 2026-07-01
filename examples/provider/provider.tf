# Prefer supplying the API key via MARMOT_API_KEY so it stays out of state.
provider "marmot" {
  host    = "https://your-marmot-host.com" # or MARMOT_HOST
  api_key = var.marmot_api_key             # or MARMOT_API_KEY
}
