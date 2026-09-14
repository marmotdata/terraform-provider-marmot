data "azurerm_client_config" "current" {}

resource "azuread_application" "marmot" {
  display_name = "marmot-azure-prod"
}

resource "marmot_secret_store_azure" "prod" {
  name      = "azure-prod"
  tenant_id = data.azurerm_client_config.current.tenant_id
  client_id = azuread_application.marmot.client_id
}

resource "azuread_application_federated_identity_credential" "marmot" {
  application_id = azuread_application.marmot.id
  display_name   = "marmot-azure-prod"
  issuer         = marmot_secret_store_azure.prod.issuer
  subject        = marmot_secret_store_azure.prod.subject
  audiences      = [marmot_secret_store_azure.prod.audience]
}

resource "azuread_service_principal" "marmot" {
  client_id = azuread_application.marmot.client_id
}

resource "azurerm_role_assignment" "marmot_reads_secrets" {
  scope                = data.azurerm_key_vault.prod.id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azuread_service_principal.marmot.object_id
}
