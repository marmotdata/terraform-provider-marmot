resource "marmot_secret_store_aws_secret" "db_password" {
  store     = marmot_secret_store_aws.prod.id
  secret_id = aws_secretsmanager_secret.db_password.arn
}
