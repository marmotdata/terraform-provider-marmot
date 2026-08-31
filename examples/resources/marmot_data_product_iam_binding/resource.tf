resource "marmot_data_product_iam_binding" "finance_readers" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog-reader"
  members         = ["group:${marmot_team.finance_analysts.id}"]
}
