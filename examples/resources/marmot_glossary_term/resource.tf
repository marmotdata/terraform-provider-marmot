resource "marmot_team" "analytics" {
  name = "analytics"
}

resource "marmot_glossary_term" "active_customer" {
  name       = "Active Customer"
  definition = "A customer with at least one order in the last 90 days."

  owner_team_ids = [marmot_team.analytics.id]

  metadata = {
    domain = "sales"
  }
}

# Terms can be organized hierarchically.
resource "marmot_glossary_term" "churned_customer" {
  name           = "Churned Customer"
  definition     = "An active customer who has not ordered in the last 90 days."
  parent_term_id = marmot_glossary_term.active_customer.id
}
