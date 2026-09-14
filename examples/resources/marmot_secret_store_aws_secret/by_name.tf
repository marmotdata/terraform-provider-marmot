resource "marmot_secret_store_aws_secret" "db_password" {
  store     = marmot_secret_store_aws.prod.id
  region    = "eu-west-1"
  secret_id = "prod/orders/db-password"
}
