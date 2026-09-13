# A grant on a data product covers every asset it resolves, including ones
# its rules match later.
resource "marmot_data_product_iam_member" "finance_reader" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog.viewer"
  member          = "group:${marmot_team.finance_analysts.id}"
}
