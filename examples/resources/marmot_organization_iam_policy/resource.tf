# Owns the catalog-wide policy. Anything not listed is revoked everywhere,
# so read the plan before applying: this can lock everyone out, you included.
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
