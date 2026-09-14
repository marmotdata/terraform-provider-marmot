resource "marmot_organization_iam_member" "analysts_organization" {
  role   = "user"
  member = "group:${marmot_team.analysts.id}"
}
