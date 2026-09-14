resource "marmot_glossary_term_iam_member" "analysts_customer" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "user"
  member           = "group:${marmot_team.analysts.id}"
}
