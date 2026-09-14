resource "google_iam_workload_identity_pool" "marmot" {
  workload_identity_pool_id = "marmot"
}

resource "google_iam_workload_identity_pool_provider" "marmot" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.marmot.workload_identity_pool_id
  workload_identity_pool_provider_id = "marmot"

  attribute_mapping = {
    "google.subject" = "assertion.sub"
  }

  oidc {
    issuer_uri = "https://acme.marmotdata.cloud"
  }
}

resource "marmot_pipeline" "analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id                 = "acme-analytics-prod"
    workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
  })

  cron_expression = "0 */6 * * *"
}

resource "google_project_iam_member" "marmot_bigquery" {
  project = "acme-analytics-prod"
  role    = "roles/bigquery.metadataViewer"
  member  = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.marmot.name}/subject/${marmot_pipeline.analytics.subject}"
}
