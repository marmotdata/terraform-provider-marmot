data "marmot_iam_policy" "organization" {
  binding {
    role    = "editor"
    members = ["serviceAccount:${marmot_service_account.etl.id}"]
  }
}

resource "marmot_organization_iam_policy" "organization" {
  policy_data = data.marmot_iam_policy.organization.policy_data
}
