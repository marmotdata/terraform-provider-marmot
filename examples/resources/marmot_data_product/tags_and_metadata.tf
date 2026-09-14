resource "marmot_data_product" "orders" {
  name = "orders"

  tags = ["orders", "commerce"]

  metadata = {
    domain = "commerce"
    tier   = "gold"
  }
}
