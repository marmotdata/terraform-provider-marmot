# Renders a policy document for the authoritative *_iam_policy resources. It
# makes no API calls; it exists so a policy can be written as HCL rather than as
# an inline JSON string.
data "marmot_iam_policy" "catalog_readers" {
  binding {
    role = "catalog-reader"
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
