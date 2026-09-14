resource "marmot_glossary_term_iam_member" "etl_customer" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "editor"
  member           = "serviceAccount:${marmot_service_account.etl.id}"
}
