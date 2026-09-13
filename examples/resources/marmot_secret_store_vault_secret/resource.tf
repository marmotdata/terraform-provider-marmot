resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
}

variable "orders_db_password" {
  type      = string
  sensitive = true
}

resource "vault_kv_secret_v2" "orders_db" {
  mount = "secret"
  name  = "orders/db"

  data_json = jsonencode({
    password = var.orders_db_password
  })
}

# One key of the secret. The key may be left out when the secret holds one.
resource "marmot_secret_store_vault_secret" "db_password" {
  store = marmot_secret_store_vault.prod.id
  mount = vault_kv_secret_v2.orders_db.mount
  name  = vault_kv_secret_v2.orders_db.name
  key   = "password"
}
