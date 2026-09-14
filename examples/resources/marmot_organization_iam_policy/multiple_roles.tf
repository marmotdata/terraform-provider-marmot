data "marmot_iam_policy" "organization" {
  binding {
    role    = "admin"
    members = ["group:${marmot_team.platform.id}"]
  }

  binding {
    role = "editor"
    members = [
      "serviceAccount:${marmot_service_account.etl.id}",
      "group:${marmot_team.analysts.id}",
    ]
  }

  binding {
    role    = "user"
    members = ["allAuthenticated"]
  }
}

resource "marmot_organization_iam_policy" "organization" {
  policy_data = data.marmot_iam_policy.organization.policy_data
}
