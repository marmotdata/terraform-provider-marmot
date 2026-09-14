resource "marmot_glossary_term_iam_member" "everyone_customer" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "user"
  member           = "allAuthenticated"
}
