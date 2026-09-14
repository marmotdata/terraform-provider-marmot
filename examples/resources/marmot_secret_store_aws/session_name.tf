resource "marmot_secret_store_aws" "prod" {
  name         = "aws-prod"
  role_arn     = aws_iam_role.marmot.arn
  session_name = "marmot-prod"
}
