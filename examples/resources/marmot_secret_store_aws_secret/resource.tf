resource "marmot_secret_store_aws" "prod" {
  name = "aws-prod"
}

resource "aws_secretsmanager_secret" "db_password" {
  name = "prod/orders/db-password"
}

# By ARN. The region comes from the ARN.
resource "marmot_secret_store_aws_secret" "db_password" {
  store     = marmot_secret_store_aws.prod.id
  secret_id = aws_secretsmanager_secret.db_password.arn
}

# By name. The region is required, and a staging label or version id pins
# what is read.
resource "aws_secretsmanager_secret" "signing_key" {
  name = "prod/orders/signing-key"
}

resource "marmot_secret_store_aws_secret" "signing_key" {
  store         = marmot_secret_store_aws.prod.id
  region        = "eu-west-1"
  secret_id     = aws_secretsmanager_secret.signing_key.name
  version_stage = "AWSPREVIOUS"
}
