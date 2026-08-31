resource "marmot_service_account" "etl" {
  name        = "orders-etl"
  description = "Ingests the orders pipeline, owned by the data platform team"
}

# Grant it what it needs per resource; it holds no catalog-wide role.
resource "marmot_data_product_iam_member" "etl_reads_finance" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog-reader"
  member          = "serviceAccount:${marmot_service_account.etl.id}"
}
