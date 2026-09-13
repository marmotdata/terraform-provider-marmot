# Owns the store's policy outright: anything not listed here is revoked on the
# next apply. Never point an _iam_binding or _iam_member at a store managed
# this way, or the two will spend every apply undoing each other.
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
