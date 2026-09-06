# Owns one role on the term. Because a grant reaches the term's children too,
# this covers the subtree beneath it as well.
resource "marmot_glossary_term_iam_binding" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  role             = "catalog.viewer"
  members          = ["group:${marmot_team.finance_analysts.id}"]
}
