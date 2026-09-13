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

# A regional secret.
resource "google_secret_manager_regional_secret" "signing_key" {
  secret_id = "signing-key"
  location  = "europe-west1"
}

resource "marmot_secret_store_google_secret" "signing_key" {
  store     = marmot_secret_store_google.prod.id
  project   = google_secret_manager_regional_secret.signing_key.project
  location  = google_secret_manager_regional_secret.signing_key.location
  secret_id = google_secret_manager_regional_secret.signing_key.secret_id
}
