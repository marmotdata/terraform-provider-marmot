resource "marmot_team" "analytics" {
  name = "analytics"

  tags = ["reporting"]

  metadata = {
    slack = "#analytics"
    lead  = "alice"
  }
}
