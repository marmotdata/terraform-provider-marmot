resource "marmot_secret_store_google" "prod" {
  name = "gcp-prod"
}

resource "marmot_secret_store_google_secret" "db_password" {
  store     = marmot_secret_store_google.prod.id
  project   = "acme-prod"
  secret_id = "orders-db-password"
}

resource "marmot_pipeline" "orders" {
  name      = "orders"
  plugin_id = "postgresql"

  config = jsonencode({
    host     = "orders-db.acme.internal"
    database = "orders"
    user     = "marmot"
  })

  secrets = {
    password = marmot_secret_store_google_secret.db_password.id
  }

  cron_expression = "0 * * * *"
}
