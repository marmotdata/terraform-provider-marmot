# Owns one role on the data product. Members not listed lose that role, while
# any other role on the product is left as it is.
resource "marmot_data_product_iam_binding" "finance_readers" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog.viewer"
  members         = ["group:${marmot_team.finance_analysts.id}"]
}
