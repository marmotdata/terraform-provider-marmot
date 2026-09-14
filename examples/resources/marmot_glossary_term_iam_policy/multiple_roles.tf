data "marmot_iam_policy" "customer" {
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

resource "marmot_glossary_term_iam_policy" "customer" {
  glossary_term_id = marmot_glossary_term.customer.id
  policy_data      = data.marmot_iam_policy.customer.policy_data
}
