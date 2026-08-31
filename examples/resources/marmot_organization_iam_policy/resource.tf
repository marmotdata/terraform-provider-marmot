# Owns the catalog-wide policy in full: anything not listed here is revoked.
# Keep at least one administrator outside this configuration, so a mistake here
# cannot lock everyone out.
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
