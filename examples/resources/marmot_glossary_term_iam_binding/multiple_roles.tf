resource "marmot_glossary_term_iam_binding" "customer_admins" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "admin"
  members          = ["group:${marmot_team.platform.id}"]
}

resource "marmot_glossary_term_iam_binding" "customer_readers" {
  glossary_term_id = marmot_glossary_term.customer.id
  role             = "user"
  members          = ["allAuthenticated"]
}
