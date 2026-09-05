# The organization is the top of the hierarchy, so this role reaches the whole
# catalog. Members left out of the list lose it everywhere.
resource "marmot_organization_iam_binding" "platform_admins" {
  role    = "admin"
  members = ["group:${marmot_team.platform.id}"]
}
