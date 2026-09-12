resource "marmot_secret_store_aws" "prod" {
  name     = "aws-prod"
  role_arn = aws_iam_role.marmot_store.arn
}

# By name: the region is required.
resource "marmot_secret_store_aws_secret" "db_password" {
  store     = marmot_secret_store_aws.prod.id
  region    = "eu-west-1"
  secret_id = "prod/orders/db-password"
}

# By ARN: the region comes from the ARN. A staging label other than
# AWSCURRENT, or a version id, pins what is read.
resource "marmot_secret_store_aws_secret" "signing_key" {
  store         = marmot_secret_store_aws.prod.id
  secret_id     = aws_secretsmanager_secret.signing_key.arn
  version_stage = "AWSPREVIOUS"
}

resource "marmot_pipeline" "postgres_orders" {
  name      = "orders"
  plugin_id = "postgresql"

  config = jsonencode({
    host     = "orders-db.acme.internal"
    database = "orders"
    user     = "marmot"
  })

  secrets = {
    password = marmot_secret_store_aws_secret.db_password.id
  }

  cron_expression = "0 * * * *"
}
