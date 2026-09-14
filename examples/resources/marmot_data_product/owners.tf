resource "marmot_data_product" "orders" {
  name = "orders"

  owner_team_ids = [marmot_team.analytics.id]
  owner_user_ids = [marmot_user.alice.id]
}
