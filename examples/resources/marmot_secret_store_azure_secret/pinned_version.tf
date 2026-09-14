resource "marmot_secret_store_azure_secret" "db_password" {
  store     = marmot_secret_store_azure.prod.id
  vault_uri = data.azurerm_key_vault.prod.vault_uri
  name      = azurerm_key_vault_secret.db_password.name
  version   = azurerm_key_vault_secret.db_password.version
}
