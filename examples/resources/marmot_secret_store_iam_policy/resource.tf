resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
}

resource "marmot_service_account" "analytics_agent" {
  name = "analytics-agent"
}

resource "marmot_team" "data_platform" {
  name = "data-platform"
}

# Owns the whole policy. Anything not listed is revoked on the next apply,
# so don't also point an _iam_binding or _iam_member at this store.
data "marmot_iam_policy" "vault_prod" {
  binding {
    role    = "secretStore.reader"
    members = ["serviceAccount:${marmot_service_account.analytics_agent.id}"]
  }

  binding {
    role    = "secretStore.user"
    members = ["group:${marmot_team.data_platform.id}"]
  }
}

resource "marmot_secret_store_iam_policy" "vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  policy_data     = data.marmot_iam_policy.vault_prod.policy_data
}
