resource "marmot_user" "alice" {
  name                = "Alice Nguyen"
  username            = "alice"
  password_wo         = ephemeral.random_password.alice.result
  password_wo_version = "1"
  profile_picture     = "https://avatars.acme.internal/alice.png"

  role_names = ["admin"]
}
