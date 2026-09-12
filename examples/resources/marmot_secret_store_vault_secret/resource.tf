resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
  role    = "marmot"
}

# One key of a KV v2 secret under the default `secret` mount.
resource "marmot_secret_store_vault_secret" "db_password" {
  store = marmot_secret_store_vault.prod.id
  name  = "orders/db"
  key   = "password"
}

# Another mount and a pinned version. The key may be left out when the
# secret holds exactly one.
resource "marmot_secret_store_vault_secret" "signing_key" {
  store   = marmot_secret_store_vault.prod.id
  mount   = "kv"
  name    = "signing/key"
  version = 3
}

resource "marmot_pipeline" "postgres_orders" {
  name      = "orders"
  plugin_id = "postgresql"

  config = jsonencode({
    host     = "orders-db.acme.internal"
    database = "orders"
    user     = "marmot"
  })

  secrets = {
    password = marmot_secret_store_vault_secret.db_password.id
  }

  cron_expression = "0 * * * *"
}
