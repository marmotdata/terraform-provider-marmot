# A grant on a term covers every term beneath it, so this covers the whole
# finance vocabulary.
resource "marmot_glossary_term_iam_member" "analysts_read_finance_terms" {
  glossary_term_id = marmot_glossary_term.finance.id
  role             = "catalog-reader"
  member           = "group:${marmot_team.analysts.id}"
}
