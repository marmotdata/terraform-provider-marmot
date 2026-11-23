resource "marmot_task" "weekly_revenue" {
  name        = "weekly-revenue"
  description = "Monday revenue digest for the finance team"
  prompt      = "Report last week's revenue by region and compare it with the week before."

  trigger = {
    type = "schedule"
    cron = "0 8 * * 1"
  }

  sources = ["warehouse"]
}
