resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
  ca_cert = file("${path.module}/vault-ca.pem")
}
