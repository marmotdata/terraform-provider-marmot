# A grant on the organization applies to the whole catalog and is inherited by
# every asset, data product and glossary term.
#
# This one states Marmot's default, that everyone can read everything. Removing
# it is what makes the catalog private, so grant the admin role first.
resource "marmot_organization_iam_member" "everyone_reads" {
  role   = "user"
  member = "allAuthenticated"
}
