# password_wo is a write-only argument, so the password never reaches state.
# Bump password_wo_version to rotate the password on a later apply.
ephemeral "random_password" "alice" {
  length = 24
}

resource "marmot_user" "alice" {
  name                = "Alice Nguyen"
  username            = "alice"
  password_wo         = ephemeral.random_password.alice.result
  password_wo_version = "1"

  role_names = ["admin"]
}

# Make the user an owner of a data product.
resource "marmot_data_product" "reporting" {
  name = "reporting"

  owner_user_ids = [marmot_user.alice.id]
}
