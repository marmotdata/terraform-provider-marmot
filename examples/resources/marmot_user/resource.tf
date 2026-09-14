ephemeral "random_password" "alice" {
  length = 24
}

resource "marmot_user" "alice" {
  name                = "Alice Nguyen"
  username            = "alice"
  password_wo         = ephemeral.random_password.alice.result
  password_wo_version = "1"
}
