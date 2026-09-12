# A store using the server's own token (VAULT_TOKEN). A private CA is
# given as PEM.
resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
  ca_cert = file("${path.module}/vault-ca.pem")
}

# Federated: Marmot presents an OIDC token for the subject `secretStore:vault-prod-federated`
# to a JWT auth role with `bound_subject` and `bound_audiences` set. The
# audience defaults to the Vault address.
resource "marmot_secret_store_vault" "federated" {
  name      = "vault-prod-federated"
  address   = "https://vault.acme.internal"
  namespace = "platform"
  role      = "marmot"
  auth_path = "jwt"
}

# The JWT role trusts the store's issuer and binds its subject and audience.
resource "vault_jwt_auth_backend_role" "marmot_store" {
  backend         = vault_jwt_auth_backend.marmot.path
  role_name       = "marmot"
  role_type       = "jwt"
  user_claim      = "sub"
  bound_subject   = marmot_secret_store_vault.federated.subject
  bound_audiences = [marmot_secret_store_vault.federated.audience]
  token_policies  = ["marmot-agents"]
}
