# Builds the policy document the *_iam_policy resources take. Makes no
# request to Marmot.
data "marmot_iam_policy" "catalog_readers" {
  binding {
    role = "catalog.viewer"
    members = [
      "serviceAccount:${marmot_service_account.etl.id}",
      "group:${marmot_team.analysts.id}",
    ]
  }

  binding {
    role    = "admin"
    members = ["user:${marmot_user.platform_lead.id}"]
  }
}
