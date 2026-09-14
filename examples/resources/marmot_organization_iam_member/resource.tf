resource "marmot_organization_iam_member" "etl_organization" {
  role   = "editor"
  member = "serviceAccount:${marmot_service_account.etl.id}"
}
