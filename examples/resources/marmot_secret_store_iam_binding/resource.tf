# Owns who holds secretStore.reader on the store. A service account left out
# of the list loses it on the next apply; other roles on the store are left
# alone, so a pipeline author's secretStore.user can be managed elsewhere.
resource "marmot_secret_store_iam_binding" "vault_prod_readers" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.reader"
  members = [
    "serviceAccount:${marmot_service_account.analytics_agent.id}",
    "serviceAccount:${marmot_service_account.ingest_agent.id}",
  ]
}
