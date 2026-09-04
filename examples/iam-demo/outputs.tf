output "teams" {
  description = "Team IDs, for constructing group: members by hand."
  value = {
    platform  = marmot_team.platform.id
    analytics = marmot_team.analytics.id
    ml        = marmot_team.ml.id
    finance   = marmot_team.finance.id
  }
}

output "service_accounts" {
  description = "Service account IDs, for constructing serviceAccount: members by hand."
  value = {
    ingest_agent = marmot_service_account.ingest_agent.id
    ai_copilot   = marmot_service_account.ai_copilot.id
    finance_etl  = marmot_service_account.finance_etl.id
  }
}

output "data_products" {
  value = {
    orders            = marmot_data_product.orders.id
    customer_360      = marmot_data_product.customer_360.id
    finance_reporting = marmot_data_product.finance_reporting.id
  }
}

# The etags show that every grant was read back from the server after writing.
output "policy_etags" {
  description = "Etag of each managed policy as last read."
  value = {
    organization       = marmot_organization_iam_binding.catalog_readers.etag
    orders_product     = marmot_data_product_iam_binding.orders_editors.etag
    finance_reporting  = marmot_data_product_iam_policy.finance_reporting.etag
    revenue_daily      = marmot_asset_iam_binding.revenue_daily_editors.etag
    catalog_api        = marmot_asset_iam_policy.catalog_api.etag
    customer_term      = marmot_glossary_term_iam_binding.customer_term_editors.etag
    recognised_revenue = marmot_glossary_term_iam_policy.recognised_revenue.etag
  }
}
