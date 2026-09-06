# Data products group the assets above into things a consumer asks for by name.
# They are also a grant target: a binding on a data product covers every asset
# it resolves, so access follows the product rather than a list of tables.

resource "marmot_data_product" "orders" {
  name        = "orders"
  description = "Order events and everything derived from them"

  tags = ["orders", "domain:commerce"]

  owner_team_ids = [marmot_team.analytics.id]
  owner_user_ids = [marmot_user.analytics_lead.id]

  metadata = {
    domain          = "commerce"
    tier            = "gold"
    sla             = "99.9"
    freshness_hours = "1"
  }
}

resource "marmot_data_product_asset" "orders_events" {
  data_product_id = marmot_data_product.orders.id
  asset_id        = marmot_asset.orders_events.id
}

resource "marmot_data_product_asset" "orders_fact" {
  data_product_id = marmot_data_product.orders.id
  asset_id        = marmot_asset.orders_fact.id
}

# Anything tagged orders joins the product without being listed by hand.
resource "marmot_data_product_rule" "orders_by_tag" {
  data_product_id = marmot_data_product.orders.id

  name             = "orders-tagged"
  description      = "Everything tagged orders"
  type             = "query"
  query_expression = "tag:orders"
}

resource "marmot_data_product" "customer_360" {
  name        = "customer-360"
  description = "The conformed customer view and the models scoring it"

  tags = ["customer", "domain:commerce"]

  owner_team_ids = [marmot_team.analytics.id, marmot_team.ml.id]

  metadata = {
    domain       = "commerce"
    tier         = "gold"
    contains_pii = "true"
  }
}

resource "marmot_data_product_asset" "customers_dim" {
  data_product_id = marmot_data_product.customer_360.id
  asset_id        = marmot_asset.customers_dim.id
}

resource "marmot_data_product_asset" "churn_model" {
  data_product_id = marmot_data_product.customer_360.id
  asset_id        = marmot_asset.churn_model.id
}

resource "marmot_data_product_rule" "customer_domain" {
  data_product_id = marmot_data_product.customer_360.id

  name           = "commerce-domain"
  description    = "Assets declaring the commerce domain"
  type           = "metadata_match"
  metadata_field = "domain"
  pattern_type   = "exact"
  pattern_value  = "commerce"
  priority       = 10
}

# The restricted one. Its policy is owned outright in iam.tf.
resource "marmot_data_product" "finance_reporting" {
  name        = "finance-reporting"
  description = "Recognised revenue and the pipeline behind it. Need-to-know."

  tags = ["finance", "restricted", "domain:finance"]

  owner_team_ids = [marmot_team.finance.id]
  owner_user_ids = [marmot_user.finance_analyst.id]

  metadata = {
    domain         = "finance"
    tier           = "gold"
    classification = "restricted"
    sox_relevant   = "true"
  }
}

resource "marmot_data_product_asset" "revenue_daily" {
  data_product_id = marmot_data_product.finance_reporting.id
  asset_id        = marmot_asset.revenue_daily.id
}

resource "marmot_data_product_asset" "exec_revenue_dashboard" {
  data_product_id = marmot_data_product.finance_reporting.id
  asset_id        = marmot_asset.exec_revenue_dashboard.id
}

resource "marmot_data_product_asset" "payments_events" {
  data_product_id = marmot_data_product.finance_reporting.id
  asset_id        = marmot_asset.payments_events.id
}
