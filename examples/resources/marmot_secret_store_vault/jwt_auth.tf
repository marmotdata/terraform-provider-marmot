resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
  role    = "marmot"
}

resource "vault_jwt_auth_backend" "marmot" {
  path               = "jwt"
  oidc_discovery_url = marmot_secret_store_vault.prod.issuer
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
  bound_subject   = marmot_secret_store_vault.prod.subject
  bound_audiences = [marmot_secret_store_vault.prod.audience]
  token_policies  = [vault_policy.marmot.name]
}
