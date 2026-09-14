resource "marmot_secret_store_iam_member" "analysts_vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.user"
  member          = "group:${marmot_team.analysts.id}"
}
