# Reads with the server's own credentials.
resource "marmot_secret_store_aws" "prod" {
  name = "aws-prod"
}

# Federated. The Marmot instance is registered as an OIDC provider and the
# role trusts the store's subject.
resource "aws_iam_openid_connect_provider" "marmot" {
  url            = "https://acme.marmotdata.cloud"
  client_id_list = ["sts.amazonaws.com"]
}

data "aws_iam_policy_document" "marmot_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.marmot.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "acme.marmotdata.cloud:sub"
      values   = ["secretStore:aws-prod-federated"]
    }

    condition {
      test     = "StringEquals"
      variable = "acme.marmotdata.cloud:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "marmot" {
  name               = "marmot-aws-prod"
  assume_role_policy = data.aws_iam_policy_document.marmot_trust.json
}

resource "marmot_secret_store_aws" "federated" {
  name     = "aws-prod-federated"
  role_arn = aws_iam_role.marmot.arn
}

resource "aws_secretsmanager_secret" "db_password" {
  name = "prod/orders/db-password"
}

data "aws_iam_policy_document" "marmot_read" {
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.db_password.arn]
  }
}

resource "aws_iam_role_policy" "marmot_read" {
  role   = aws_iam_role.marmot.name
  policy = data.aws_iam_policy_document.marmot_read.json
}
