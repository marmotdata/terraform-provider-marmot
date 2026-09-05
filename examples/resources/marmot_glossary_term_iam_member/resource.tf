# A grant on a term also covers everything beneath it, so granting at the top of
# a subtree opens the whole vocabulary under it without naming each child.
resource "marmot_glossary_term_iam_member" "finance_vocabulary" {
  glossary_term_id = marmot_glossary_term.finance.id
  role             = "catalog.viewer"
  member           = "group:${marmot_team.finance_analysts.id}"
}
