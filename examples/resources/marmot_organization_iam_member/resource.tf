# Organization is the root of the hierarchy: a grant here applies to the whole
# catalog and is inherited by every asset, data product and glossary term.
#
# allAuthenticated is how Marmot's default — everyone can read everything — is
# stated explicitly. Removing this resource is what locks an instance down.
resource "marmot_organization_iam_member" "everyone_reads" {
  role   = "user"
  member = "allAuthenticated"
}
