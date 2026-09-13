# Keyless: the pipeline presents its own identity, exchanged at a Workload
# Identity Federation provider that trusts the Marmot instance as an OIDC
# issuer. Marmot Cloud or Marmot Enterprise. Grant the pipeline's subject
# on the project; no service account key exists anywhere.
resource "marmot_pipeline" "bigquery_analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id                 = "acme-analytics-prod"
    workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
  })

  cron_expression = "0 */6 * * *" # every six hours
  enabled         = true
}

resource "google_project_iam_member" "marmot_bigquery" {
  project = "acme-analytics-prod"
  role    = "roles/bigquery.metadataViewer"
  member  = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.marmot.name}/subject/${marmot_pipeline.bigquery_analytics.subject}"
}

# Credentials come from a secret store. The value is injected into config
# at the key before each run and never enters state.
resource "marmot_secret_store_google" "prod" {
  name = "gcp-prod"
}

resource "google_secret_manager_secret" "db_password" {
  secret_id = "orders-db-password"

  replication {
    auto {}
  }
}

resource "marmot_secret_store_google_secret" "db_password" {
  store     = marmot_secret_store_google.prod.id
  project   = google_secret_manager_secret.db_password.project
  secret_id = google_secret_manager_secret.db_password.secret_id
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
