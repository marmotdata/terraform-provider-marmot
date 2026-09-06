# Access grants.
#
# Each grant comes in three strengths, following the google_*_iam_* family. An
# _iam_policy owns everything on its target, an _iam_binding owns a single role
# and leaves the rest alone, and an _iam_member adds one member to one role and
# touches nothing else. The weaker the resource, the more happily it shares a
# target with another team's configuration.
#
# You can grant on four things, from the top down: the organization, a data
# product, an asset, or a glossary term. Grants flow downhill, so a data product
# covers the assets it resolves and a glossary term covers its children.
#
# Because grants only ever add and nothing denies, the model below keeps the
# root deliberately thin: a read-only baseline everyone shares, with anything
# that can change the catalog granted per resource. Access a principal must not
# have is simply never granted in the first place.

# ---------------------------------------------------------------------------
# Organization: the baseline everyone stands on.
# ---------------------------------------------------------------------------

# Everyone in the company gets read-only access, and this list is the whole of
# it. A team added to the `user` role by hand is removed again on the next
# apply.
resource "marmot_organization_iam_binding" "catalog_readers" {
  role = "user"
  members = [
    "group:${marmot_team.platform.id}",
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
    "group:${marmot_team.finance.id}",
  ]
}

# Platform are the catalog administrators. This adds them without claiming to
# know who else should hold `admin`, so it will not fight another configuration
# that grants it too.
resource "marmot_organization_iam_member" "platform_admins" {
  role   = "admin"
  member = "group:${marmot_team.platform.id}"
}

# ---------------------------------------------------------------------------
# Data products: access follows the product, so it keeps up as tables come and
# go.
# ---------------------------------------------------------------------------

# Analytics can edit everything the orders product resolves, and this list
# decides who else can.
resource "marmot_data_product_iam_binding" "orders_editors" {
  data_product_id = marmot_data_product.orders.id
  role            = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "user:${marmot_user.analytics_lead.id}",
  ]
}

# ML also work on customer-360, but analytics manage their own grants on it
# elsewhere. A member resource adds one without disturbing the other, which is
# what you want whenever two configurations share a target.
resource "marmot_data_product_iam_member" "ml_edits_customer_360" {
  data_product_id = marmot_data_product.customer_360.id
  role            = "editor"
  member          = "group:${marmot_team.ml.id}"
}

# finance-reporting carries recognised revenue, so nothing here is left to
# chance: this block is the complete list of who can reach it, and a grant added
# outside Terraform is reverted on the next apply.
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
# Assets: the narrowest thing you can grant on.
# ---------------------------------------------------------------------------

# Finance and their ETL account can rebuild the revenue table. Because this
# list is authoritative, nobody picks up write access to it by being added to
# `editor` somewhere else.
resource "marmot_asset_iam_binding" "revenue_daily_editors" {
  asset_id = marmot_asset.revenue_daily.id
  role     = "editor"
  members = [
    "group:${marmot_team.finance.id}",
    "serviceAccount:${marmot_service_account.finance_etl.id}",
  ]
}

# The copilot has no organization role at all, so this one grant is the entire
# extent of what it can read: a single topic, and nothing else in the catalog.
resource "marmot_asset_iam_member" "copilot_reads_orders" {
  asset_id = marmot_asset.orders_events.id
  role     = "user"
  member   = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

# The catalog API is meant to be visible to everyone internally, so its policy
# names allAuthenticated as a reader while keeping write access with platform.
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
# Glossary terms: granting on a term also grants everything beneath it.
# ---------------------------------------------------------------------------

# Granted on the parent term, which is why Active Customer and Churned Customer
# are covered without appearing anywhere below.
resource "marmot_glossary_term_iam_binding" "customer_term_editors" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "editor"
  members = [
    "group:${marmot_team.analytics.id}",
    "group:${marmot_team.ml.id}",
  ]
}

# The copilot's second and final grant. It can read the GMV definition, which
# is enough to explain the measure, and nothing else in the glossary.
resource "marmot_glossary_term_iam_member" "copilot_reads_gmv" {
  glossary_term_id = marmot_glossary_term.gmv.id
  role             = "user"
  member           = "serviceAccount:${marmot_service_account.ai_copilot.id}"
}

# Recognised Revenue is the definition of record for the board pack. Finance
# own it and analytics may read it, and owning the policy outright is what keeps
# that true over time.
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
