resource "marmot_secret_store_iam_binding" "vault_prod_admins" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.user"
  members         = ["group:${marmot_team.platform.id}"]
}

resource "marmot_secret_store_iam_binding" "vault_prod_readers" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.viewer"
  members         = ["allAuthenticated"]
}
