resource "marmot_asset" "orders" {
  name        = "orders"
  type        = "Table"
  description = "One row per order placed in the storefront"
  services    = ["PostgreSQL"]
}
