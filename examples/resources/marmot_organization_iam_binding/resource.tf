# The organization is the root of the hierarchy, so this role applies across
# the whole catalog.
resource "marmot_organization_iam_binding" "platform_admins" {
  role    = "admin"
  members = ["group:${marmot_team.platform.id}"]
}
