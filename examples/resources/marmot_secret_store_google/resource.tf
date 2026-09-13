# Reads with the server's own credentials.
resource "marmot_secret_store_google" "prod" {
  name = "gcp-prod"
}

# Federated. The pool trusts the Marmot instance as an OIDC issuer and the
# store exchanges its token at the provider.
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

resource "marmot_secret_store_google" "federated" {
  name                       = "gcp-prod-federated"
  workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
}

resource "google_secret_manager_secret" "db_password" {
  secret_id = "orders-db-password"

  replication {
    auto {}
  }
}

# Grant the store's subject on each secret it reads.
resource "google_secret_manager_secret_iam_member" "marmot" {
  secret_id = google_secret_manager_secret.db_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.marmot.name}/subject/${marmot_secret_store_google.federated.subject}"
}
