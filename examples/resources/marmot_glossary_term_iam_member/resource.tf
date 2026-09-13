# A grant on a term covers its descendants.
resource "marmot_glossary_term_iam_member" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  role             = "catalog.viewer"
  member           = "group:${marmot_team.finance_analysts.id}"
}
