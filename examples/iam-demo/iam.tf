# Access grants, following the google_*_iam_* family: _iam_policy owns the
# whole policy, _iam_binding owns one role, _iam_member adds one member to
# one role. Grants flow down: a data product covers the assets it resolves,
# a glossary term covers its children. There are no denies, so the root gets
# a read-only baseline and everything else is granted per resource.

# Organization.

# Read-only access for everyone. A team added to `user` by hand is removed on
# the next apply.
resource "marmot_organization_iam_binding" "catalog_readers" {
  role = "user"
  members = [
    "group:${marmot_team.platform.id}",
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
    "group:${marmot_team.finance.id}",
  ]
}

# Platform administer the catalog. A member resource, so another
# configuration can grant `admin` too.
resource "marmot_organization_iam_member" "platform_admins" {
  role   = "admin"
  member = "group:${marmot_team.platform.id}"
}

# Data products.

# Everyone who can edit what the orders product resolves.
resource "marmot_data_product_iam_binding" "orders_editors" {
  data_product_id = marmot_data_product.orders.id
  role            = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "user:${marmot_user.analytics_lead.id}",
  ]
}

# Analytics manage their own grants on customer-360 elsewhere; a member
# resource adds ML without touching those.
resource "marmot_data_product_iam_member" "ml_edits_customer_360" {
  data_product_id = marmot_data_product.customer_360.id
  role            = "editor"
  member          = "group:${marmot_team.ml.id}"
}

# The complete policy on finance-reporting. A grant added outside Terraform
# is reverted on the next apply.
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

# Assets.

# Everyone who can edit the revenue table.
resource "marmot_asset_iam_binding" "revenue_daily_editors" {
  asset_id = marmot_asset.revenue_daily.id
  role     = "editor"
  members = [
    "group:${marmot_team.finance.id}",
    "serviceAccount:${marmot_service_account.finance_etl.id}",
  ]
}

# The copilot has no organization role, so this is all it can read.
resource "marmot_asset_iam_member" "copilot_reads_orders" {
  asset_id = marmot_asset.orders_events.id
  role     = "user"
  member   = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

# Readable by everyone, editable by platform.
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

# Glossary terms.

# Granted on the parent term, so it covers Active Customer and Churned
# Customer too.
resource "marmot_glossary_term_iam_binding" "customer_term_editors" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
  ]
}

# The copilot's only glossary grant.
resource "marmot_glossary_term_iam_member" "copilot_reads_gmv" {
  glossary_term_id = marmot_glossary_term.gmv.id
  role             = "user"
  member           = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

# Finance own the definition, analytics read it.
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
