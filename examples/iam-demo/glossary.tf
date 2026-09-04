# Business terms. A grant on a term covers its descendants, so the hierarchy
# below is also an access hierarchy.

resource "marmot_glossary_term" "customer" {
  name       = "Customer"
  definition = "A legal person that has completed at least one checkout on an Acme storefront."

  owner_team_ids = [marmot_team.analytics.id]

  metadata = {
    domain = "commerce"
    status = "approved"
  }
}

resource "marmot_glossary_term" "active_customer" {
  name           = "Active Customer"
  definition     = "A Customer with at least one delivered order in the trailing 90 days."
  description    = "Used as the denominator in retention and churn reporting."
  parent_term_id = marmot_glossary_term.customer.id

  owner_team_ids = [marmot_team.analytics.id]

  metadata = {
    domain = "commerce"
    status = "approved"
  }
}

resource "marmot_glossary_term" "churned_customer" {
  name           = "Churned Customer"
  definition     = "An Active Customer whose most recent delivered order is more than 90 days old."
  parent_term_id = marmot_glossary_term.customer.id

  owner_team_ids = [marmot_team.ml.id]

  metadata = {
    domain = "ml"
    status = "approved"
  }
}

resource "marmot_glossary_term" "gmv" {
  name        = "Gross Merchandise Value"
  definition  = "The total value of orders placed, before refunds, discounts and tax."
  description = "Not a revenue measure. Do not use it in external reporting."

  owner_team_ids = [marmot_team.analytics.id]

  metadata = {
    domain = "commerce"
    status = "approved"
  }
}

resource "marmot_glossary_term" "recognised_revenue" {
  name        = "Recognised Revenue"
  definition  = "Revenue recognised under ASC 606 once the performance obligation is satisfied."
  description = "The definition of record for the board pack. Changes go through finance."

  owner_team_ids = [marmot_team.finance.id]

  metadata = {
    domain         = "finance"
    status         = "approved"
    classification = "restricted"
    sox_relevant   = "true"
  }
}
