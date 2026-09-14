resource "marmot_secret_store_iam_binding" "vault_prod_editors" {
  secret_store_id = marmot_secret_store_vault.prod.id
  role            = "secretStore.reader"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
