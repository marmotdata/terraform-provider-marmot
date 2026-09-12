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

# Credentials stay out of config and out of state: the value is read from
# the store before each run and injected into config at the key.
resource "marmot_pipeline" "postgres_orders" {
  name      = "orders"
  plugin_id = "postgresql"

  config = jsonencode({
    host     = "orders-db.acme.internal"
    database = "orders"
    user     = "marmot"
  })

  secret {
    key   = "password"
    store = marmot_secret_store_google.prod.id
    ref   = jsonencode({ secret = "orders-db-password", version = "latest" })
  }

  cron_expression = "0 * * * *"
}
