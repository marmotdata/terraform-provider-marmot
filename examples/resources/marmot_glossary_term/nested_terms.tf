resource "marmot_glossary_term" "customer" {
  name       = "Customer"
  definition = "Anyone who has placed at least one order."
}

resource "marmot_glossary_term" "active_customer" {
  name           = "Active Customer"
  definition     = "A customer with at least one order in the last 90 days."
  parent_term_id = marmot_glossary_term.customer.id
}

resource "marmot_glossary_term" "churned_customer" {
  name           = "Churned Customer"
  definition     = "A customer with no order in the last 90 days."
  parent_term_id = marmot_glossary_term.customer.id
}
