resource "marmot_secret_store_azure" "prod" {
  name      = "azure-prod"
  tenant_id = data.azurerm_client_config.current.tenant_id
  client_id = azuread_application.marmot_store.client_id
}

# The latest version of a secret.
resource "marmot_secret_store_azure_secret" "db_password" {
  store     = marmot_secret_store_azure.prod.id
  vault_uri = azurerm_key_vault.prod.vault_uri
  name      = "orders-db-password"
}

# A pinned version.
resource "marmot_secret_store_azure_secret" "signing_key" {
  store     = marmot_secret_store_azure.prod.id
  vault_uri = azurerm_key_vault.prod.vault_uri
  name      = "signing-key"
  version   = "6f2a1c9e8d4b4f1e9c3a7b5d2e8f0a1b"
}

resource "marmot_pipeline" "postgres_orders" {
  name      = "orders"
  plugin_id = "postgresql"

  config = jsonencode({
    host     = "orders-db.acme.internal"
    database = "orders"
    user     = "marmot"
  })

  secrets = {
    password = marmot_secret_store_azure_secret.db_password.id
  }

  cron_expression = "0 * * * *"
}
