# Builds the JSON document the authoritative *_iam_policy resources expect.
# Nothing is sent to Marmot when it runs; it exists so a policy can be written
# as ordinary HCL blocks instead of a hand-written JSON string.
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
