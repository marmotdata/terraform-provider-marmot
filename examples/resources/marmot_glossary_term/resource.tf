resource "marmot_glossary_term" "active_customer" {
  name       = "Active Customer"
  definition = "A customer with at least one order in the last 90 days."
}
