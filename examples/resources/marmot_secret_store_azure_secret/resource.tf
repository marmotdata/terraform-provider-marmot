resource "marmot_secret_store_azure_secret" "db_password" {
  store     = marmot_secret_store_azure.prod.id
  vault_uri = "https://acme-prod.vault.azure.net"
  name      = "orders-db-password"
}
