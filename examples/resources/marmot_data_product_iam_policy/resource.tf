# Owns the whole policy of the data product and everything it resolves.
data "marmot_iam_policy" "finance" {
  binding {
    role    = "catalog.viewer"
    members = ["group:${marmot_team.finance_analysts.id}"]
  }
}

resource "marmot_data_product_iam_policy" "finance" {
  data_product_id = marmot_data_product.finance.id
  policy_data     = data.marmot_iam_policy.finance.policy_data
}
