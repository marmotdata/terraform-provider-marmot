resource "marmot_organization_iam_binding" "platform_admins" {
  role    = "admin"
  members = ["group:${marmot_team.platform.id}"]
}
