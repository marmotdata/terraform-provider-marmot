resource "marmot_asset" "orders" {
  name     = "orders"
  type     = "Table"
  services = ["PostgreSQL"]

  tags = ["orders", "domain:commerce"]

  metadata = {
    domain       = "commerce"
    owner        = "platform"
    contains_pii = "true"
  }
}
