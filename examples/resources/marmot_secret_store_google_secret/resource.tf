resource "marmot_secret_store_google" "prod" {
  name                       = "gcp-prod"
  workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
}

# The latest version of a global secret.
resource "marmot_secret_store_google_secret" "db_password" {
  store     = marmot_secret_store_google.prod.id
  project   = "acme-secrets"
  secret_id = "orders-db-password"
}

# A pinned version of a regional secret.
resource "marmot_secret_store_google_secret" "signing_key" {
  store     = marmot_secret_store_google.prod.id
  project   = "acme-secrets"
  location  = "europe-west1"
  secret_id = "signing-key"
  version   = "3"
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
    password = marmot_secret_store_google_secret.db_password.id
  }

  cron_expression = "0 * * * *"
}
