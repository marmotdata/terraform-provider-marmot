# The teams, users and service accounts iam.tf grants to.

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

# The only user with a catalog-wide admin role.
resource "marmot_user" "platform_lead" {
  name                = "Dana Okafor"
  username            = "dana"
  password_wo         = var.demo_password
  password_wo_version = "1"

  role_names = ["admin"]
}

# Everyone else is read-only at the root and granted per resource.
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

# Service accounts hold no organization role; iam.tf grants them per resource.
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

# A key for CI. The plaintext is kept in state as the sensitive `key` attribute.
resource "marmot_service_account_api_key" "ingest_agent_ci" {
  service_account_id = marmot_service_account.ingest_agent.id
  name               = "ci-runner"
  expires_in_days    = 90
}
