resource "marmot_task" "schema_watch" {
  name   = "schema-watch"
  prompt = "Describe what changed in the schema and which downstream assets it may break."

  trigger = {
    type   = "event"
    events = ["asset.schema_changed"]

    filter = {
      asset_types = ["table"]
      providers   = ["bigquery"]
    }

    cooldown_seconds = 900
  }
}
