resource "marmot_glossary_term_iam_member" "alice_customer" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "admin"
  member           = "user:${marmot_user.alice.id}"
}
