resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
}

resource "marmot_service_account" "analytics_agent" {
  name = "analytics-agent"
}

resource "marmot_service_account" "ingest_agent" {
  name = "ingest-agent"
}

# Owns one role on the store. Anyone left out loses it on the next apply;
# other roles are untouched.
resource "marmot_secret_store_iam_binding" "vault_prod_readers" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.reader"
  members = [
    "serviceAccount:${marmot_service_account.analytics_agent.id}",
    "serviceAccount:${marmot_service_account.ingest_agent.id}",
  ]
}
