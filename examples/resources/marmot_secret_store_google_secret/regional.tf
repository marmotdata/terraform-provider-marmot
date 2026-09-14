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
