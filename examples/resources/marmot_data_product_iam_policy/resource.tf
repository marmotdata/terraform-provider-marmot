data "marmot_iam_policy" "orders" {
  binding {
    role    = "editor"
    members = ["serviceAccount:${marmot_service_account.etl.id}"]
  }
}

resource "marmot_data_product_iam_policy" "orders" {
  data_product_id = marmot_data_product.orders.id
  policy_data     = data.marmot_iam_policy.orders.policy_data
}
