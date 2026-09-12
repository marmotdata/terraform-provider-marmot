resource "marmot_service_account" "analytics_agent" {
  name = "analytics-agent"
}

# Where in Vault the key is written.
resource "marmot_secret_store_vault_secret" "analytics_agent_key" {
  store = marmot_secret_store_vault.prod.id
  name  = "agents/analytics/marmot"
  key   = "api_key"
}

# Marmot mints a short-lived API key for the account, writes it to Vault at
# the secret, and renews it every half hour. The agent reads the key from
# Vault with its own identity; no long-lived credential exists anywhere.
resource "marmot_service_account_lease" "analytics_agent" {
  service_account_id = marmot_service_account.analytics_agent.id
  secret             = marmot_secret_store_vault_secret.analytics_agent_key.id
  ttl_seconds        = 3600
}
