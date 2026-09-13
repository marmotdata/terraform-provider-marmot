resource "marmot_secret_store_azure" "prod" {
  name = "azure-prod"
}

data "azurerm_key_vault" "prod" {
  name                = "acme-prod"
  resource_group_name = "prod"
}

variable "orders_db_password" {
  type      = string
  sensitive = true
}

resource "azurerm_key_vault_secret" "db_password" {
  name         = "orders-db-password"
  value        = var.orders_db_password
  key_vault_id = data.azurerm_key_vault.prod.id
}

resource "marmot_secret_store_azure_secret" "db_password" {
  store     = marmot_secret_store_azure.prod.id
  vault_uri = data.azurerm_key_vault.prod.vault_uri
  name      = azurerm_key_vault_secret.db_password.name
}
