data "marmot_iam_policy" "vault_prod" {
  binding {
    role    = "secretStore.reader"
    members = ["serviceAccount:${marmot_service_account.etl.id}"]
  }
}

resource "marmot_secret_store_iam_policy" "vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  policy_data     = data.marmot_iam_policy.vault_prod.policy_data
}
