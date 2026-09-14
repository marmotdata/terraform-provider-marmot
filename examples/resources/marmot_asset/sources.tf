resource "marmot_asset" "orders" {
  name     = "orders"
  type     = "Table"
  services = ["PostgreSQL"]

  sources = [{
    name     = "postgresql"
    priority = 1
    properties = {
      host     = "orders-db.acme.internal"
      database = "orders"
      schema   = "public"
    }
  }]
}
