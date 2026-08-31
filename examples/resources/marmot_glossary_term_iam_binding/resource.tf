# A grant on a term covers every term beneath it, so this binds the whole
# finance vocabulary.
resource "marmot_glossary_term_iam_binding" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  role             = "catalog-reader"
  members          = ["group:${marmot_team.analysts.id}"]
}
