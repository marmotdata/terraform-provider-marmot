# A grant at the organization reaches every asset, data product and glossary
# term in the catalog.
#
# allAuthenticated spells out Marmot's default of letting any signed-in user
# read everything. Removing this resource is what closes an instance down.
resource "marmot_organization_iam_member" "everyone_reads" {
  role   = "user"
  member = "allAuthenticated"
}
