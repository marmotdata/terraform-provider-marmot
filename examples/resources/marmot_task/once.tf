# Runs one time, as soon as it is created.
resource "marmot_task" "orphaned_tables" {
  name   = "orphaned-tables"
  prompt = "List the tables without an owner, largest first."

  trigger = {
    type = "once"
  }
}

# Runs one time, at the given moment.
resource "marmot_task" "year_end_summary" {
  name   = "year-end-summary"
  prompt = "Summarise this year's revenue by quarter."

  trigger = {
    type   = "once"
    run_at = "2026-12-31T17:00:00Z"
  }
}
