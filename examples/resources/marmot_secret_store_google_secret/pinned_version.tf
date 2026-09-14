resource "marmot_secret_store_google_secret" "db_password" {
  store     = marmot_secret_store_google.prod.id
  project   = "acme-prod"
  secret_id = "orders-db-password"
  version   = "3"
}
