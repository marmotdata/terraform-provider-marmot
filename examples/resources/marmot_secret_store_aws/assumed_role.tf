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
      values   = ["secretStore:aws-prod"]
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

resource "marmot_secret_store_aws" "prod" {
  name     = "aws-prod"
  role_arn = aws_iam_role.marmot.arn
}

data "aws_iam_policy_document" "marmot_read" {
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = ["arn:aws:secretsmanager:eu-west-1:123456789012:secret:prod/*"]
  }
}

resource "aws_iam_role_policy" "marmot_read" {
  role   = aws_iam_role.marmot.name
  policy = data.aws_iam_policy_document.marmot_read.json
}
