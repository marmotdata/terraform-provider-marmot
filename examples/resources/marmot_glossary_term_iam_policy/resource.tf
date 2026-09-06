# Owns the term's policy outright. Anything not listed here is revoked, for the
# term and for its descendants.
data "marmot_iam_policy" "finance_vocabulary" {
  binding {
    role    = "catalog.viewer"
    members = ["group:${marmot_team.finance_analysts.id}"]
  }
}

resource "marmot_glossary_term_iam_policy" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  policy_data      = data.marmot_iam_policy.finance_vocabulary.policy_data
}
