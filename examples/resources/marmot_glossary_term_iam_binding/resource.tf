# Owns one role on the term and its descendants.
resource "marmot_glossary_term_iam_binding" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  role             = "catalog.viewer"
  members          = ["group:${marmot_team.finance_analysts.id}"]
}
