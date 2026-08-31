# Grants the ETL account read access to the finance data product, and with it
# every asset the product resolves. This is how a whole domain is granted
# without listing its assets.
resource "marmot_data_product_iam_member" "etl_reads_finance" {
  data_product_id = marmot_data_product.finance.id
  role            = "catalog-reader"
  member          = "serviceAccount:${marmot_service_account.etl.id}"
}
