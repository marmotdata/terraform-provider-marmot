data "marmot_iam_policy" "vault_prod" {
  binding {
    role    = "secretStore.user"
    members = ["group:${marmot_team.platform.id}"]
  }

  binding {
    role = "secretStore.reader"
    members = [
      "serviceAccount:${marmot_service_account.etl.id}",
      "group:${marmot_team.analysts.id}",
    ]
  }

  binding {
    role    = "secretStore.viewer"
    members = ["allAuthenticated"]
  }
}

resource "marmot_secret_store_iam_policy" "vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  policy_data     = data.marmot_iam_policy.vault_prod.policy_data
}
