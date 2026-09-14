resource "marmot_secret_store_vault_secret" "db_password" {
  store = marmot_secret_store_vault.prod.id
  name  = "orders/db"
  key   = "password"
}
