---
page_title: "Keyless pipelines"
subcategory: "Guides"
description: |-
  Pipelines that authenticate to Google Cloud, AWS or Azure as themselves, with no key stored in Marmot or in Terraform state.
---

# Keyless pipelines

~> **Requires Marmot Cloud or Marmot Enterprise.** On open source Marmot the credential is part of the pipeline's `config`, encrypted at rest on the server.

A pipeline is a plugin pointed at a source, run on a schedule. For a source in Google Cloud, AWS or Azure it does not need a credential at all: your instance is an OIDC issuer, it mints a short-lived token before each run, and your cloud exchanges that token for access.

Two read-only attributes make this work:

| Attribute | What it is |
| --- | --- |
| `issuer` | Your instance's URL. Register it as an OIDC provider on the cloud side. |
| `subject` | `pipeline:{name}`. Grant this on the cloud side. |

Reference both from the cloud-side grant rather than writing them out. A rename then updates both ends in one apply, instead of leaving a grant pointing at a subject that no longer exists.

## Google Cloud

Trust the issuer with a Workload Identity Pool provider, then grant the pipeline's subject directly on the resource.

```terraform
resource "google_iam_workload_identity_pool" "marmot" {
  workload_identity_pool_id = "marmot"
}

resource "google_iam_workload_identity_pool_provider" "marmot" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.marmot.workload_identity_pool_id
  workload_identity_pool_provider_id = "marmot"

  attribute_mapping = {
    "google.subject" = "assertion.sub"
  }

  oidc {
    issuer_uri = "https://acme.marmotdata.cloud"
  }
}

resource "marmot_pipeline" "analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id                 = "acme-analytics-prod"
    workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
  })

  cron_expression = "0 */6 * * *"
}

resource "google_project_iam_member" "marmot_bigquery" {
  project = "acme-analytics-prod"
  role    = "roles/bigquery.metadataViewer"
  member  = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.marmot.name}/subject/${marmot_pipeline.analytics.subject}"
}
```

Granting the federated principal directly is the shorter path and the one to prefer. Add `service_account` to the plugin config only when something downstream needs a real service account identity, and grant that account `roles/iam.workloadIdentityUser` for the subject.

Use a read-only role. `roles/bigquery.metadataViewer` lets the plugin read schemas and nothing else; a catalog has no reason to hold `dataViewer`.

## AWS

Register the issuer as an IAM OIDC provider with the client id `sts.amazonaws.com`, then allow `sts:AssumeRoleWithWebIdentity` for the pipeline's subject.

```terraform
locals {
  pipeline_name = "glue-catalog"
}

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
      values   = ["pipeline:${local.pipeline_name}"]
    }
  }
}

resource "aws_iam_role" "marmot_glue" {
  name               = "marmot-glue-catalog"
  assume_role_policy = data.aws_iam_policy_document.marmot_trust.json
}

resource "marmot_pipeline" "glue_catalog" {
  name      = local.pipeline_name
  plugin_id = "glue"

  config = jsonencode({
    credentials = {
      region   = "eu-west-1"
      role_arn = aws_iam_role.marmot_glue.arn
    }
  })

  cron_expression = "0 */6 * * *"
}
```

The trust policy holds the subject as a literal, because a trust policy cannot depend on the role it trusts without a cycle. Holding the pipeline name in a `local` and using it on both sides keeps them from drifting apart.

Attach a read-only policy to the role — `AWSGlueConsoleReadOnlyAccess` or your own with just `glue:Get*` and `glue:List*`.

## Azure

Add a federated identity credential to an app registration whose issuer is your instance, and whose subject is the pipeline.

```terraform
locals {
  pipeline_name = "blob-inventory"
}

resource "azuread_application" "marmot" {
  display_name = "marmot-ingestion"
}

resource "azuread_application_federated_identity_credential" "marmot" {
  application_id = azuread_application.marmot.id
  display_name   = "marmot-${local.pipeline_name}"
  issuer         = "https://acme.marmotdata.cloud"
  subject        = "pipeline:${local.pipeline_name}"
  audiences      = ["api://AzureADTokenExchange"]
}

resource "azuread_service_principal" "marmot" {
  client_id = azuread_application.marmot.client_id
}

resource "azurerm_role_assignment" "marmot_blob" {
  scope                = azurerm_storage_account.analytics.id
  role_definition_name = "Storage Blob Data Reader"
  principal_id         = azuread_service_principal.marmot.object_id
}

resource "marmot_pipeline" "blob_inventory" {
  name      = local.pipeline_name
  plugin_id = "azureblob"

  config = jsonencode({
    account_name = azurerm_storage_account.analytics.name
    tenant_id    = data.azurerm_client_config.current.tenant_id
    client_id    = azuread_application.marmot.client_id
  })

  cron_expression = "0 */6 * * *"
}
```

An app registration takes several federated credentials, so one registration can serve every pipeline. Give each its own credential rather than a wildcard subject: revoking one pipeline's access is then deleting one resource.

## One pipeline per source

The subject is the pipeline's name, so the pipeline is the unit of access on the cloud side. Two sources sharing a pipeline share a grant and cannot be revoked separately. Splitting them costs one more resource and buys a boundary.

## What the plugin needs

Each plugin puts the federation fields in its own place in `config`. The [plugin registry](https://plugins.marmotdata.io) lists, per plugin, where the field sits and which read-only role to grant. The pattern is always the same: Google takes `workload_identity_provider`, AWS takes a role to assume, and Azure takes a tenant and client id with no account key.

## Sources with no cloud identity

A PostgreSQL database behind a password cannot federate. That credential belongs in your secret manager, read by Marmot just before each run — see [secret stores](secret-stores.md).

## Verifying it

`issuer` and `subject` are `null` on a server with no workload identity issuer, which is the first thing to check if a plan shows nothing to grant. After an apply, run the pipeline from the Runs page rather than waiting for its cron: a federation problem shows up as an exchange failure in the run log, naming the side that refused.
