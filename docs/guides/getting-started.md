---
page_title: "Getting started with the Marmot provider"
subcategory: "Guides"
description: |-
  Point the provider at your instance, declare your first resources, and understand which parts of a catalog belong in Terraform.
---

# Getting started

This guide takes you from nothing to a catalog you manage in code: a provider block, a data product with assets in it, and a pipeline that keeps the catalog in step with the system it describes.

## Point the provider at your instance

```terraform
terraform {
  required_version = ">= 1.11"

  required_providers {
    marmot = {
      source = "marmotdata/marmot"
    }
  }
}

provider "marmot" {
  host = "https://acme.marmotdata.cloud"
}
```

On your own machine, leave the credential out. With no `api_key` and no `token`, the provider falls back to the session `marmot login` wrote, which is a short-lived token rather than a key on disk.

```console
$ marmot login https://acme.marmotdata.cloud
$ terraform plan
```

In CI, set `MARMOT_HOST` and `MARMOT_API_KEY` in the environment and the provider block needs no arguments at all:

```terraform
provider "marmot" {}
```

The key belongs to a service account created for that repository. See [service accounts and CI](service-accounts.md) for how to create one, scope it, and keep the key out of state.

Terraform >= 1.11 is worth requiring. Ephemeral resources and write-only arguments are what let a key reach a secret manager without ever landing in a state file, and several resources here are built around them.

## Declare something

Start with one asset so the loop is short:

```terraform
resource "marmot_asset" "orders" {
  name        = "orders"
  type        = "Table"
  services    = ["PostgreSQL"]
  description = "One row per placed order. The system of record for revenue."

  tags = ["tier-1", "revenue"]

  metadata = {
    owner    = "commerce-eng"
    database = "orders_db"
    schema   = "public"
  }

  schema = {
    order_id    = "uuid"
    buyer_id    = "uuid"
    total_cents = "bigint"
    placed_at   = "timestamptz"
  }
}
```

```console
$ terraform apply
```

The asset is now in the catalog, and the next `plan` is empty. If someone edits its description in the UI, the plan after that is not empty, which is the point: the configuration is the source of truth and drift shows up as a diff.

## Group assets into a data product

A data product is the unit people actually ask for. Declare the product, then say what belongs to it.

```terraform
resource "marmot_data_product" "revenue" {
  name        = "Revenue"
  description = "Everything the finance team reconciles against. Changes here need their sign-off."

  owner_team_ids = [marmot_team.finance.id]
  tags           = ["tier-1"]
}

resource "marmot_data_product_asset" "revenue_orders" {
  data_product_id = marmot_data_product.revenue.id
  asset_id        = marmot_asset.orders.id
}
```

For membership that should keep working as the catalog grows, use a rule instead of listing assets by hand:

```terraform
resource "marmot_data_product_rule" "revenue_marts" {
  data_product_id  = marmot_data_product.revenue.id
  name             = "revenue marts"
  type             = "query"
  query_expression = "@metadata.layer = marts"
}
```

Assets matching the rule join the product as they appear, and a grant on the product reaches them the moment they do.

## Record lineage between them

```terraform
resource "marmot_asset" "fct_orders" {
  name     = "fct_orders"
  type     = "Model"
  services = ["dbt"]
}

resource "marmot_lineage" "orders_to_fct" {
  source = marmot_asset.orders.mrn
  target = marmot_asset.fct_orders.mrn
}
```

`source` and `target` are MRNs, not IDs — every asset exposes its own as `mrn`.

Declare the edges your plugins cannot see. Lineage a plugin discovers for itself does not belong in Terraform, where it would only go stale.

## Let a pipeline do the discovery

Declaring assets by hand is right for a handful of things you own and wrong for a warehouse with ten thousand tables. Point a plugin at the source and let it catalog what it finds:

```terraform
resource "marmot_pipeline" "analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id = "acme-analytics-prod"
  })

  cron_expression = "0 */6 * * *"
}
```

The rule of thumb: **a pipeline discovers, Terraform decides**. Schemas, columns and technical descriptions come from the source on every run. Ownership, data products, glossary terms and access are decisions somebody made, and those belong in a repository where the decision is reviewed.

On Marmot Cloud that pipeline can run without a credential at all. See [keyless pipelines](keyless-pipelines.md).

## What to declare next

| Guide | Covers |
| --- | --- |
| [Access control](access-control.md) | Who can reach what, with roles bound on the organization or one resource. |
| [Keyless pipelines](keyless-pipelines.md) | Pipelines that authenticate to Google Cloud, AWS or Azure as themselves. |
| [Secret stores](secret-stores.md) | Sources that need a password, read from your vault at run time. |
| [Service accounts and CI](service-accounts.md) | Running this configuration from CI without leaking a key. |

## A layout that scales

One repository, one state file, and the catalog split by what changes together:

```
catalog/
  main.tf            provider, backend
  teams.tf           marmot_team, marmot_user
  service_accounts.tf
  iam.tf             every grant, in one reviewable place
  products/
    revenue.tf
    buyer360.tf
  pipelines/
    warehouse.tf
    streaming.tf
```

Keeping every grant in one file is worth the small inconvenience. "Who can read Buyer 360" should be answerable by reading one file rather than by grepping the repository.
