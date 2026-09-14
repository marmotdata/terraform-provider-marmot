resource "marmot_secret_store_aws_secret" "signing_key_previous" {
  store         = marmot_secret_store_aws.prod.id
  secret_id     = aws_secretsmanager_secret.signing_key.arn
  version_stage = "AWSPREVIOUS"
}
