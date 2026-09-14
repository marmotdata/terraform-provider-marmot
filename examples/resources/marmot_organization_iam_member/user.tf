resource "marmot_organization_iam_member" "alice_organization" {
  role   = "admin"
  member = "user:${marmot_user.alice.id}"
}
