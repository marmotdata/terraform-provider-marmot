resource "marmot_asset" "orders" {
  name     = "orders"
  type     = "Table"
  services = ["PostgreSQL"]

  environments = {
    prod = {
      name = "Production"
      path = "orders-db.acme.internal/orders/public/orders"
      metadata = {
        region = "eu-west-1"
      }
    }
    staging = {
      name = "Staging"
      path = "orders-db.staging.acme.internal/orders/public/orders"
    }
  }
}
