# Logs in with the server's own VAULT_TOKEN.
resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
}

# Federated. A JWT auth method trusts the Marmot instance and its role
# binds the store's subject and audience.
resource "marmot_secret_store_vault" "federated" {
  name    = "vault-prod-federated"
  address = "https://vault.acme.internal"
  role    = "marmot"
}

resource "vault_jwt_auth_backend" "marmot" {
  path               = "jwt"
  oidc_discovery_url = marmot_secret_store_vault.federated.issuer
}

resource "vault_policy" "marmot" {
  name = "marmot"

  policy = <<-EOT
    path "secret/data/orders/*" {
      capabilities = ["read"]
    }
  EOT
}

resource "vault_jwt_auth_backend_role" "marmot" {
  backend         = vault_jwt_auth_backend.marmot.path
  role_name       = "marmot"
  role_type       = "jwt"
  user_claim      = "sub"
  bound_subject   = marmot_secret_store_vault.federated.subject
  bound_audiences = [marmot_secret_store_vault.federated.audience]
  token_policies  = [vault_policy.marmot.name]
}
