variable "alice_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "alice_password_version" {
  type    = string
  default = "1"
}

resource "marmot_user" "alice" {
  name                = "Alice Nguyen"
  username            = "alice"
  password_wo         = var.alice_password
  password_wo_version = var.alice_password_version
}
