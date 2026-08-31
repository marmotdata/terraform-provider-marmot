data "marmot_iam_policy" "finance_vocabulary" {
  binding {
    role    = "catalog-reader"
    members = ["group:${marmot_team.analysts.id}"]
  }
}

resource "marmot_glossary_term_iam_policy" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  policy_data      = data.marmot_iam_policy.finance_vocabulary.policy_data
}
