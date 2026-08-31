# A grant on a term covers its descendants, so binding the top of a subtree
# grants the whole vocabulary beneath it.
resource "marmot_glossary_term_iam_member" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  role             = "catalog-reader"
  member           = "group:${marmot_team.finance_analysts.id}"
}
