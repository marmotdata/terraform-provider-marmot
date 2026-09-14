resource "marmot_glossary_term" "active_customer" {
  name        = "Active Customer"
  definition  = "A customer with at least one order in the last 90 days."
  description = "Used by retention reporting and the churn model."

  owner_team_ids = [marmot_team.analytics.id]
  owner_user_ids = [marmot_user.alice.id]

  metadata = {
    domain = "sales"
  }
}
