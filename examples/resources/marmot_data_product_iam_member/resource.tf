# A grant on a data product reaches every asset the product contains, including
# ones that start matching its rules later. This is how you give a team a whole
# domain without naming each table, and how that access keeps up as the domain
# grows.
resource "marmot_data_product_iam_member" "finance_reader" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog.viewer"
  member          = "group:${marmot_team.finance_analysts.id}"
}
