resource "marmot_pipeline" "analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id = "acme-analytics-prod"
  })

  cron_expression = "0 */6 * * *"
  enabled         = false
}
