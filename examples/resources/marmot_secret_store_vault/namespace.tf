resource "marmot_secret_store_vault" "prod" {
  name      = "vault-prod"
  address   = "https://vault.acme.internal"
  namespace = "data-platform"
  auth_path = "marmot"
  role      = "marmot"
}
