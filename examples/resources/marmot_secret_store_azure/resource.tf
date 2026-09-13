# Reads with the server's own credentials.
resource "marmot_secret_store_azure" "prod" {
  name = "azure-prod"
}

# Federated. An app registration whose federated credential trusts the
# store's token.
data "azurerm_client_config" "current" {}

resource "azuread_application" "marmot" {
  display_name = "marmot-azure-prod"
}

resource "marmot_secret_store_azure" "federated" {
  name      = "azure-prod-federated"
  tenant_id = data.azurerm_client_config.current.tenant_id
  client_id = azuread_application.marmot.client_id
}

resource "azuread_application_federated_identity_credential" "marmot" {
  application_id = azuread_application.marmot.id
  display_name   = "marmot-azure-prod"
  issuer         = marmot_secret_store_azure.federated.issuer
  subject        = marmot_secret_store_azure.federated.subject
  audiences      = [marmot_secret_store_azure.federated.audience]
}

resource "azuread_service_principal" "marmot" {
  client_id = azuread_application.marmot.client_id
}

data "azurerm_key_vault" "prod" {
  name                = "acme-prod"
  resource_group_name = "prod"
}

resource "azurerm_role_assignment" "marmot_reads_secrets" {
  scope                = data.azurerm_key_vault.prod.id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azuread_service_principal.marmot.object_id
}
