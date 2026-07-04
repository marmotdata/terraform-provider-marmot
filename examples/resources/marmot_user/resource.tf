# password_wo is a write-only argument, so the password never reaches state.
# Bump password_wo_version to rotate the password on a later apply.
ephemeral "random_password" "svc" {
  length = 24
}

resource "marmot_user" "svc" {
  name                = "Catalog Service"
  username            = "svc-catalog"
  password_wo         = ephemeral.random_password.svc.result
  password_wo_version = "1"

  role_names = ["admin"]
}

# Make the user an owner of a data product.
resource "marmot_data_product" "reporting" {
  name = "reporting"

  owner_user_ids = [marmot_user.svc.id]
}
