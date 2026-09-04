# Access grants.
#
# Three authority levels, mirroring the google_*_iam_* family:
#
#   _iam_policy   owns the whole policy on a resource. Anything absent is removed.
#   _iam_binding  owns one role on a resource. Other roles are left alone.
#   _iam_member   owns one (role, member) pair and nothing else.
#
# Four targets, from the root down: organization, data product, asset, glossary
# term. A grant on a data product covers every asset it resolves; a grant on a
# glossary term covers its descendants.
#
# Grants are additive and there are no denies. So the shape of the model below
# is: a thin read-only baseline at the root, and everything that can change the
# catalog granted per resource. Something a principal must not reach is simply
# never granted at the root.

# ---------------------------------------------------------------------------
# Organization: the baseline everyone stands on.
# ---------------------------------------------------------------------------

# Authoritative for the `user` role across the whole catalog: read-only. Any
# group added to this role out of band is removed on the next apply.
resource "marmot_organization_iam_binding" "catalog_readers" {
  role = "user"
  members = [
    "group:${marmot_team.platform.id}",
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
    "group:${marmot_team.finance.id}",
  ]
}

# Non-authoritative: adds the platform team as catalog administrators without
# claiming ownership of who else holds `admin`.
resource "marmot_organization_iam_member" "platform_admins" {
  role   = "admin"
  member = "group:${marmot_team.platform.id}"
}

# ---------------------------------------------------------------------------
# Data products: access follows the product, not a list of tables.
# ---------------------------------------------------------------------------

# Analytics can edit everything the orders product resolves. Authoritative for
# `editor` on that product.
resource "marmot_data_product_iam_binding" "orders_editors" {
  data_product_id = marmot_data_product.orders.id
  role            = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "user:${marmot_user.analytics_lead.id}",
  ]
}

# One grant on customer-360, leaving analytics' own grants on it untouched.
# This is the resource to reach for when two configurations share a target.
resource "marmot_data_product_iam_member" "ml_edits_customer_360" {
  data_product_id = marmot_data_product.customer_360.id
  role            = "editor"
  member          = "group:${marmot_team.ml.id}"
}

# finance-reporting is restricted, so its policy is owned outright: this is the
# complete list of who can reach recognised revenue, and an out-of-band grant is
# reverted on the next apply.
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

# ---------------------------------------------------------------------------
# Assets: the narrowest grants.
# ---------------------------------------------------------------------------

# Authoritative for `editor` on the revenue table. The ETL account can rebuild
# it; nobody else picks up write access by being added to the role elsewhere.
resource "marmot_asset_iam_binding" "revenue_daily_editors" {
  asset_id = marmot_asset.revenue_daily.id
  role     = "editor"
  members = [
    "group:${marmot_team.finance.id}",
    "serviceAccount:${marmot_service_account.finance_etl.id}",
  ]
}

# The copilot holds no organization role at all. This single grant is the whole
# of its read access to the catalog: one topic, nothing else.
resource "marmot_asset_iam_member" "copilot_reads_orders" {
  asset_id = marmot_asset.orders_events.id
  role     = "user"
  member   = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

# The product catalog API is public within the organization, so its policy names
# allAuthenticated as a reader and keeps write access with the platform team.
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

# ---------------------------------------------------------------------------
# Glossary terms: a grant on a term covers its children.
# ---------------------------------------------------------------------------

# Granted on the parent term, so it reaches Active Customer and Churned Customer
# without either being named here.
resource "marmot_glossary_term_iam_binding" "customer_term_editors" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
  ]
}

# The copilot's second and last grant: it can read the GMV definition so it can
# explain the measure, and it can read nothing else in the glossary.
resource "marmot_glossary_term_iam_member" "copilot_reads_gmv" {
  glossary_term_id = marmot_glossary_term.gmv.id
  role             = "user"
  member           = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

# Recognised Revenue is the definition of record for the board pack: finance
# owns it, analytics may read it, and the policy is authoritative so that stays
# true.
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
