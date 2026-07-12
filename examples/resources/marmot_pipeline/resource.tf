resource "marmot_pipeline" "bigquery_analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id              = "acme-analytics-prod"
    use_default_credentials = true
  })

  cron_expression = "0 */6 * * *" # every six hours
  enabled         = true
}