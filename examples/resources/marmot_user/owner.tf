resource "marmot_data_product" "reporting" {
  name = "reporting"

  owner_user_ids = [marmot_user.alice.id]
}
