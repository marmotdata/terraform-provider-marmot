resource "marmot_team" "analytics" {
  name = "analytics"
}

resource "marmot_data_product" "reporting" {
  name = "reporting"

  owner_team_ids = [marmot_team.analytics.id]
}
