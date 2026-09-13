resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
}

resource "marmot_service_account" "analytics_agent" {
  name = "analytics-agent"
}

# Adds one member to one role and leaves the rest of the policy alone.
# secretStore.reader lets the account read secret values through this store.
resource "marmot_secret_store_iam_member" "agent_reads_vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.reader"
  member          = "serviceAccount:${marmot_service_account.analytics_agent.id}"
}
