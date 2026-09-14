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
