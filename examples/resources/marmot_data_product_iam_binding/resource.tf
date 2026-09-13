# Owns one role on the data product. Anyone left out loses it on the next
# apply; other roles are untouched.
resource "marmot_data_product_iam_binding" "finance_readers" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog.viewer"
  members         = ["group:${marmot_team.finance_analysts.id}"]
}
