data "marmot_iam_policy" "finance" {
  binding {
    role = "catalog-reader"
    members = [
      "group:${marmot_team.analysts.id}",
      "serviceAccount:${marmot_service_account.etl.id}",
    ]
  }
}

resource "marmot_data_product_iam_policy" "finance" {
  data_product_id = marmot_data_product.finance.id
  policy_data     = data.marmot_iam_policy.finance.policy_data
}
