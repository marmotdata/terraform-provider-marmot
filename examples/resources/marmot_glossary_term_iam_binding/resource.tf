resource "marmot_glossary_term_iam_binding" "customer_editors" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "editor"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
