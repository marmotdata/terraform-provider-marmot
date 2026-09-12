resource "marmot_service_account" "analytics_agent" {
  name = "analytics-agent"
}

# Marmot mints a short-lived API key for the account, writes it to Vault at
# the ref, and renews it every half hour. The agent reads the key from Vault
# with its own identity; no long-lived credential exists anywhere.
resource "marmot_service_account_lease" "analytics_agent" {
  service_account_id = marmot_service_account.analytics_agent.id
  store              = marmot_secret_store_vault.prod.id
  ref                = jsonencode({ path = "agents/analytics/marmot", key = "api_key" })
  ttl_seconds        = 3600
}
