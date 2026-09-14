resource "marmot_asset" "orders" {
  name     = "orders"
  type     = "Table"
  services = ["PostgreSQL"]

  external_links = [
    {
      name = "Runbook"
      url  = "https://docs.acme.internal/runbooks/orders"
      icon = "book"
    },
    {
      name = "Dashboard"
      url  = "https://grafana.acme.internal/d/orders"
      icon = "chart"
    },
  ]
}
