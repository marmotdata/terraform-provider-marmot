resource "marmot_asset" "orders_events" {
  name     = "commerce.orders.events"
  type     = "Topic"
  services = ["Kafka"]

  schema = {
    type      = "avro"
    name      = "OrderEvent"
    namespace = "com.acme.commerce"
    fields = jsonencode([
      { name = "order_id", type = "string" },
      { name = "customer_id", type = "string" },
      { name = "total_minor_units", type = "long" },
    ])
  }
}
