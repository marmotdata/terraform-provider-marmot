# A store using the server's own credentials (Application Default
# Credentials). Refs name a secret in this project unless they set their own.
resource "marmot_secret_store_google" "prod" {
  name    = "gcp-prod"
  project = "acme-secrets"
}

# Federated: Marmot presents an OIDC token for the subject `secretStore:gcp-prod`,
# which a Workload Identity Federation provider trusting the Marmot issuer
# exchanges for a credential granted only the secrets this store serves.
# The audience is derived from the provider by the server.
resource "marmot_secret_store_google" "federated" {
  name    = "gcp-prod-federated"
  project = "acme-secrets"

  auth {
    method                     = "federated"
    workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
  }
}

# Grant the store's subject on each secret it may read. The pool and
# provider are created once per project, trusting the store's `issuer`.
resource "google_secret_manager_secret_iam_member" "db_password" {
  secret_id = google_secret_manager_secret.db_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.marmot.name}/subject/${marmot_secret_store_google.federated.subject}"
}
