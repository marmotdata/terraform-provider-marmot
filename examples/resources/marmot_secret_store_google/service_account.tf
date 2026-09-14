resource "google_service_account" "marmot" {
  account_id = "marmot-secrets"
}

resource "marmot_secret_store_google" "prod" {
  name                       = "gcp-prod"
  workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
  service_account            = google_service_account.marmot.email
}

resource "google_service_account_iam_member" "marmot" {
  service_account_id = google_service_account.marmot.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.marmot.name}/subject/${marmot_secret_store_google.prod.subject}"
}

resource "google_secret_manager_secret_iam_member" "marmot" {
  secret_id = google_secret_manager_secret.db_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.marmot.email}"
}
