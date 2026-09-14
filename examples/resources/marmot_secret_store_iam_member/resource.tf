resource "marmot_secret_store_iam_member" "etl_vault_prod" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.reader"
  member          = "serviceAccount:${marmot_service_account.etl.id}"
}
