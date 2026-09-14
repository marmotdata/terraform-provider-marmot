resource "marmot_organization_iam_binding" "catalog_readers" {
  role = "user"
  members = [
    "group:${marmot_team.platform.id}",
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
    "group:${marmot_team.finance.id}",
  ]
}

resource "marmot_organization_iam_member" "platform_admins" {
  role   = "admin"
  member = "group:${marmot_team.platform.id}"
}

resource "marmot_data_product_iam_binding" "orders_editors" {
  data_product_id = marmot_data_product.orders.id
  role            = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "user:${marmot_user.analytics_lead.id}",
  ]
}

resource "marmot_data_product_iam_member" "ml_edits_customer_360" {
  data_product_id = marmot_data_product.customer_360.id
  role            = "editor"
  member          = "group:${marmot_team.ml.id}"
}

data "marmot_iam_policy" "finance_reporting" {
  binding {
    role = "editor"
    members = [
      "group:${marmot_team.finance.id}",
      "serviceAccount:${marmot_service_account.finance_etl.id}",
    ]
  }

  binding {
    role    = "admin"
    members = ["user:${marmot_user.platform_lead.id}"]
  }
}

resource "marmot_data_product_iam_policy" "finance_reporting" {
  data_product_id = marmot_data_product.finance_reporting.id
  policy_data     = data.marmot_iam_policy.finance_reporting.policy_data
}

resource "marmot_asset_iam_binding" "revenue_daily_editors" {
  asset_id = marmot_asset.revenue_daily.id
  role     = "editor"
  members = [
    "group:${marmot_team.finance.id}",
    "serviceAccount:${marmot_service_account.finance_etl.id}",
  ]
}

resource "marmot_asset_iam_member" "copilot_reads_orders" {
  asset_id = marmot_asset.orders_events.id
  role     = "user"
  member   = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

data "marmot_iam_policy" "catalog_api" {
  binding {
    role    = "user"
    members = ["allAuthenticated"]
  }

  binding {
    role = "editor"
    members = [
      "group:${marmot_team.platform.id}",
      "serviceAccount:${marmot_service_account.ingest_agent.id}",
    ]
  }
}

resource "marmot_asset_iam_policy" "catalog_api" {
  asset_id    = marmot_asset.catalog_api.id
  policy_data = data.marmot_iam_policy.catalog_api.policy_data
}

resource "marmot_glossary_term_iam_binding" "customer_term_editors" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
  ]
}

resource "marmot_glossary_term_iam_member" "copilot_reads_gmv" {
  glossary_term_id = marmot_glossary_term.gmv.id
  role             = "user"
  member           = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

data "marmot_iam_policy" "recognised_revenue" {
  binding {
    role    = "editor"
    members = ["group:${marmot_team.finance.id}"]
  }

  binding {
    role = "user"
    members = [
      "group:${marmot_team.analytics.id}",
      "user:${marmot_user.platform_lead.id}",
    ]
  }
}

resource "marmot_glossary_term_iam_policy" "recognised_revenue" {
  glossary_term_id = marmot_glossary_term.recognised_revenue.id
  policy_data      = data.marmot_iam_policy.recognised_revenue.policy_data
}
