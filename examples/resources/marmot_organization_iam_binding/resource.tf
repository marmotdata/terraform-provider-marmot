resource "marmot_organization_iam_binding" "organization_editors" {
  role = "editor"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "group:${marmot_team.analysts.id}",
  ]
}
