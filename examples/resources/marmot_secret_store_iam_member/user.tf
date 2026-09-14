resource "marmot_secret_store_iam_member" "alice_vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.viewer"
  member          = "user:${marmot_user.alice.id}"
}
