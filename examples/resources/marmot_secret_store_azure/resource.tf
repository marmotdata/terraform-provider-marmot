# A store using the server's own credentials (DefaultAzureCredential, a
# managed identity in a pod).
resource "marmot_secret_store_azure" "prod" {
  name = "azure-prod"
}

# Federated: Marmot presents an OIDC token for the subject `secretStore:azure-prod-federated`
# to an app registration carrying a federated credential that trusts the
# Marmot issuer. The audience defaults to `api://AzureADTokenExchange`.
resource "marmot_secret_store_azure" "federated" {
  name      = "azure-prod-federated"
  tenant_id = data.azurerm_client_config.current.tenant_id
  client_id = azuread_application.marmot_store.client_id
}

# The federated credential binds the store's issuer, subject and audience.
resource "azuread_application_federated_identity_credential" "marmot_store" {
  application_id = azuread_application.marmot_store.id
  display_name   = "marmot-store-azure-prod"
  issuer         = marmot_secret_store_azure.federated.issuer
  subject        = marmot_secret_store_azure.federated.subject
  audiences      = [marmot_secret_store_azure.federated.audience]
}
