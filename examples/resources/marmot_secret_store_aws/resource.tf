# A store using the server's own credentials (the default AWS credential
# chain, IRSA in a pod).
resource "marmot_secret_store_aws" "prod" {
  name   = "aws-prod"
  region = "eu-west-1"
}

# Federated: Marmot presents an OIDC token for the subject `secretStore:aws-prod`
# and assumes a role whose trust policy names the Marmot issuer as an OIDC
# provider and conditions on that subject. The audience defaults to
# `sts.amazonaws.com`, the client ID registered on the OIDC provider.
resource "marmot_secret_store_aws" "federated" {
  name   = "aws-prod-federated"
  region = "eu-west-1"

  auth {
    method   = "federated"
    role_arn = aws_iam_role.marmot_store.arn
  }
}

# The role trusts the store's subject and audience at the Marmot issuer. The
# subject is `store:{name}`, spelled out because the role must exist before
# the store that assumes it.
locals {
  marmot_issuer_host = trimprefix(aws_iam_openid_connect_provider.marmot.url, "https://")
}

data "aws_iam_policy_document" "marmot_store_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.marmot.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.marmot_issuer_host}:sub"
      values   = ["secretStore:aws-prod-federated"]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.marmot_issuer_host}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}
