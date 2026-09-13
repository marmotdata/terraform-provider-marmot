# A grant at the organization reaches the whole catalog. allAuthenticated
# is Marmot's default: any signed-in user can read everything.
resource "marmot_organization_iam_member" "everyone_reads" {
  role   = "user"
  member = "allAuthenticated"
}
