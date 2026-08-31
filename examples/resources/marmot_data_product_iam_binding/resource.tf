# A grant on a data product reaches every asset the product resolves, including
# assets added to it later.
resource "marmot_data_product_iam_binding" "finance_readers" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog-reader"
  members = [
    "group:${marmot_team.analysts.id}",
    "serviceAccount:${marmot_service_account.etl.id}",
  ]
}
