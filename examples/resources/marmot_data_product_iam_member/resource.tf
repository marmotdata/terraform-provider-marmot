# A grant on a data product reaches every asset the product resolves, through
# its rules and its manual members alike. This is how you grant access to a
# whole domain without listing its assets — and how that access keeps up as the
# domain grows.
resource "marmot_data_product_iam_member" "finance_reader" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog-reader"
  member          = "group:${marmot_team.finance_analysts.id}"
}
