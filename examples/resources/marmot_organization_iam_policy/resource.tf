# This owns the catalog-wide policy in full, so anything missing from it is
# revoked everywhere at once. Read the plan carefully before applying: this is
# the one resource that can lock every user out, including you.
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
