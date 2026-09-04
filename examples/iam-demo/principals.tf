# Principals: the teams, users and service accounts that the access grants in
# iam.tf refer to. Nothing here carries a broad organization role except the
# platform lead, so every other capability in the catalog is granted per
# resource.

resource "marmot_team" "platform" {
  name        = "platform"
  description = "Runs the ingestion plane and the catalog itself"

  tags = ["infrastructure"]

  metadata = {
    slack     = "#platform"
    oncall    = "platform-oncall"
    cost_code = "ENG-001"
  }
}

resource "marmot_team" "analytics" {
  name        = "analytics"
  description = "Owns the reporting datasets and the orders data product"

  tags = ["reporting", "sql"]

  metadata = {
    slack     = "#analytics"
    cost_code = "DATA-014"
  }
}

resource "marmot_team" "ml" {
  name        = "ml"
  description = "Trains and serves the propensity and recommendation models"

  tags = ["models"]

  metadata = {
    slack     = "#ml-platform"
    cost_code = "DATA-021"
  }
}

resource "marmot_team" "finance" {
  name        = "finance"
  description = "Owns recognised revenue reporting; its data is need-to-know"

  tags = ["restricted", "reporting"]

  metadata = {
    slack     = "#finance-data"
    cost_code = "FIN-003"
  }
}

# The only human with a catalog-wide administrative role.
resource "marmot_user" "platform_lead" {
  name                = "Dana Okafor"
  username            = "dana"
  password_wo         = var.demo_password
  password_wo_version = "1"

  role_names = ["admin"]
}

# Everyone else gets the read-only baseline role and is lifted per resource.
resource "marmot_user" "analytics_lead" {
  name                = "Priya Raman"
  username            = "priya"
  password_wo         = var.demo_password
  password_wo_version = "1"

  role_names = ["user"]
}

resource "marmot_user" "ml_engineer" {
  name                = "Tomas Lindqvist"
  username            = "tomas"
  password_wo         = var.demo_password
  password_wo_version = "1"

  role_names = ["user"]
}

resource "marmot_user" "finance_analyst" {
  name                = "Wei Chen"
  username            = "wei"
  password_wo         = var.demo_password
  password_wo_version = "1"

  role_names = ["user"]
}

# Machine principals. None of them hold an organization-level role: everything
# they can do comes from the grants in iam.tf, which is the point of the demo.
resource "marmot_service_account" "ingest_agent" {
  name        = "orders-ingest-agent"
  description = "Writes the orders and payments topics into the catalog"
}

resource "marmot_service_account" "ai_copilot" {
  name        = "catalog-copilot"
  description = "Answers questions over the catalog; scoped to what it is granted"
}

resource "marmot_service_account" "finance_etl" {
  name        = "finance-revenue-etl"
  description = "Builds the recognised revenue tables"
}

# A durable key slot for the ingest agent. The plaintext key never enters state.
resource "marmot_service_account_api_key" "ingest_agent_ci" {
  service_account_id = marmot_service_account.ingest_agent.id
  name               = "ci-runner"
  expires_in_days    = 90
}
