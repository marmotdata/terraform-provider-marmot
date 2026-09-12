resource "marmot_pipeline" "bigquery_analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id              = "acme-analytics-prod"
    use_default_credentials = true
  })

  cron_expression = "0 */6 * * *" # every six hours
  enabled         = true
}

# Credentials stay out of config and out of state: the secret is read from
# the store before each run and injected into config at the key.
resource "marmot_secret_store_google_secret" "orders_db_password" {
  store     = marmot_secret_store_google.prod.id
  project   = "acme-secrets"
  secret_id = "orders-db-password"
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
    password = marmot_secret_store_google_secret.orders_db_password.id
  }

  cron_expression = "0 * * * *"
}
