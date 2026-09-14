resource "marmot_secret_store_vault_secret" "signing_key" {
  store   = marmot_secret_store_vault.prod.id
  mount   = "kv-orders"
  name    = "signing-key"
  version = 2
}
