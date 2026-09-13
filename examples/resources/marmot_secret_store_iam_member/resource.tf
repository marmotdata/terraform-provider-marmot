# A service account reads a secret's value through the store's identity, and
# needs secretStore.reader on that store to do so. Granting it per store keeps
# the account's reach obvious: it sees nothing registered elsewhere.
resource "marmot_secret_store_iam_member" "agent_reads_vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.reader"
  member          = "serviceAccount:${marmot_service_account.analytics_agent.id}"
}
