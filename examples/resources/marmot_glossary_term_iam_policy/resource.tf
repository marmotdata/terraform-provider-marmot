data "marmot_iam_policy" "customer" {
  binding {
    role    = "editor"
    members = ["serviceAccount:${marmot_service_account.etl.id}"]
  }
}

resource "marmot_glossary_term_iam_policy" "customer" {
  glossary_term_id = marmot_glossary_term.customer.id
  policy_data      = data.marmot_iam_policy.customer.policy_data
}
