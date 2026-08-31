# Owns the catalog-wide policy in full. Anything not listed here is revoked, so
# take care: this is the resource that can lock everyone out.
data "marmot_iam_policy" "organization" {
  binding {
    role    = "admin"
    members = ["group:${marmot_team.platform.id}"]
  }
  binding {
    role    = "user"
    members = ["allAuthenticated"]
  }
}

resource "marmot_organization_iam_policy" "organization" {
  policy_data = data.marmot_iam_policy.organization.policy_data
}
