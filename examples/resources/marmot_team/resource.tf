resource "marmot_team" "analytics" {
  name        = "analytics"
  description = "Owns the reporting datasets"

  tags = ["reporting"]

  metadata = {
    slack = "#analytics"
  }
}

# Make the team an owner of a data product.
resource "marmot_data_product" "reporting" {
  name = "reporting"

  owner_team_ids = [marmot_team.analytics.id]
}
