resource "marmot_organization_iam_binding" "organization_admins" {
  role    = "admin"
  members = ["group:${marmot_team.platform.id}"]
}

resource "marmot_organization_iam_binding" "organization_readers" {
  role    = "user"
  members = ["allAuthenticated"]
}
