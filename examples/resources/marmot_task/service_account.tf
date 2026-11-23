resource "marmot_service_account" "revenue_reporter" {
  name        = "revenue-reporter"
  description = "Runs the weekly revenue task. Owned by the finance team."
}

resource "marmot_task" "weekly_revenue" {
  name   = "weekly-revenue"
  prompt = "Report last week's revenue by region and compare it with the week before."

  trigger = {
    type = "schedule"
    cron = "0 8 * * 1"
  }

  run_as_service_account_id = marmot_service_account.revenue_reporter.id
}
