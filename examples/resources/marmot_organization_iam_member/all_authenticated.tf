resource "marmot_organization_iam_member" "everyone_organization" {
  role   = "user"
  member = "allAuthenticated"
}
