resource "marmot_task" "weekly_revenue" {
  name   = "weekly-revenue"
  prompt = "Report last week's revenue by region and compare it with the week before."

  trigger = {
    type = "schedule"
    cron = "0 8 * * 1"
  }

  # What is left out takes the instance default.
  limits = {
    max_queries     = 5
    timeout_seconds = 300
  }
}
