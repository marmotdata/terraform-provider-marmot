# Set MARMOT_API_KEY instead of api_key to keep the key out of state.
provider "marmot" {
  host    = "https://your-marmot-host.com" # or MARMOT_HOST
  api_key = var.marmot_api_key             # or MARMOT_API_KEY
}
