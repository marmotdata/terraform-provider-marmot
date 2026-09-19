---
page_title: "Access control with Terraform"
subcategory: "Guides"
description: |-
  The IAM model, when to use member, binding or policy, and a recommended setup that keeps the organization narrow and grants everything else on the resource that needs it.
---

# Access control

~> **Requires Marmot Cloud or Marmot Enterprise.** Open-source Marmot grants roles across the whole instance and has no access-policy API, so the `*_iam_*` resources fail on apply there.

Marmot's access model is the one Google Cloud IAM uses. A **role** is a bundle of permissions, a **binding** gives one member one role on one resource, and a **policy** is the set of bindings on a resource. Grants add up, nothing subtracts, and there are no deny rules.

## The hierarchy

| Resource | Terraform resources | A grant here covers |
| --- | --- | --- |
| Organization | `marmot_organization_iam_*` | The whole instance, including resources created later. |
| Data product | `marmot_data_product_iam_*` | The product and every asset in it, now and later. |
| Asset | `marmot_asset_iam_*` | One asset. The usual shape for an agent. |
| Glossary term | `marmot_glossary_term_iam_*` | The term and every term nested under it. |
| Secret store | `marmot_secret_store_iam_*` | One store and the secrets registered in it. |

Because grants inherit downward and nothing subtracts, the organization level sets a floor you cannot dig below. Keep it narrow.

## Members

| Member | Who |
| --- | --- |
| `group:<team id>` | A team. Everyone in it, now and later. |
| `serviceAccount:<id>` | CI, an ingestion identity, an agent. |
| `user:<id>` | One person. |
| `allAuthenticated` | Everyone who can sign in. |

Bind to teams rather than to people. A grant to a team keeps meaning the right thing as people join and leave, and it is one resource instead of one per person.

## member, binding or policy

Each resource type comes in three forms. They differ only in how much of the policy Terraform claims to own.

| Resource | Authoritative over | Use when |
| --- | --- | --- |
| `*_iam_member` | One member in one role. | **Almost always.** |
| `*_iam_binding` | Every member of one role. | You need the member list for a role to be exactly what the configuration says. |
| `*_iam_policy` | The entire policy. | A resource whose access is owned end to end by this configuration and nothing else. |

Start with `_iam_member`. It adds one grant and touches nothing else, so it coexists with grants made in the UI, by another configuration, or by a team that owns its own product. A stray grant someone added by hand is then visible in the access panel rather than silently reverted on the next apply.

Reach for `_iam_binding` when "only these three service accounts may write here" is a statement you want enforced, and `_iam_policy` when the resource is sensitive enough that anything not in the configuration should disappear. Never mix `_iam_policy` with `_iam_member` or `_iam_binding` on the same resource: they will fight, and each apply will undo the other.

```terraform
# Non-authoritative: adds this grant, leaves the rest of the policy alone.
resource "marmot_data_product_iam_member" "finance_edits_revenue" {
  data_product_id = marmot_data_product.revenue.id
  role            = "dataProduct.editor"
  member          = "group:${marmot_team.finance.id}"
}

# Authoritative for one role: these two service accounts and no others.
resource "marmot_asset_iam_binding" "orders_writers" {
  asset_id = marmot_asset.orders.id
  role     = "asset.editor"
  members = [
    "serviceAccount:${marmot_service_account.etl.id}",
    "serviceAccount:${marmot_service_account.backfill.id}",
  ]
}

# Authoritative for everything: the complete policy on this asset.
data "marmot_iam_policy" "pii_exports" {
  binding {
    role    = "asset.dataViewer"
    members = ["group:${marmot_team.data_governance.id}"]
  }
}

resource "marmot_asset_iam_policy" "pii_exports" {
  asset_id    = marmot_asset.pii_exports.id
  policy_data = data.marmot_iam_policy.pii_exports.policy_data
}
```

## The roles worth knowing

The full list is in the [access control documentation](https://marmotdata.io/docs/Cloud/access-control). Four distinctions carry most of the weight:

- `asset.viewer` sees that a table exists and what its columns mean. `asset.dataViewer` also reads sample rows. Grant the first broadly and the second narrowly.
- `catalog.viewer` reads assets, data products and the glossary. It is the right floor for most instances.
- `catalog.none` grants nothing. Set it as the default role when an instance must be closed by default.
- `secretStore.user` can point a pipeline at a secret. `secretStore.reader` can see the value. Almost nobody who writes pipelines needs the second.

Editing roles bind at the organization only, because write permissions are checked instance-wide. Read and access-policy roles bind anywhere in the hierarchy.

## A recommended setup

Read access at the organization, everything else on the resource that needs it. Nothing here grants an editing role at the root, so no team can edit outside what it was given.

```terraform
resource "marmot_organization_iam_member" "everyone_reads" {
  role   = "catalog.viewer"
  member = "allAuthenticated"
}

resource "marmot_organization_iam_member" "platform_admin" {
  role   = "admin"
  member = "group:${marmot_team.platform.id}"
}

resource "marmot_data_product_iam_member" "finance_edits_revenue" {
  data_product_id = marmot_data_product.revenue.id
  role            = "dataProduct.editor"
  member          = "group:${marmot_team.finance.id}"
}

resource "marmot_asset_iam_member" "copilot_reads_orders" {
  asset_id = marmot_asset.orders.id
  role     = "asset.viewer"
  member   = "serviceAccount:${marmot_service_account.copilot.id}"
}
```

If the catalog holds something not everyone should read, **replace the floor rather than subtracting from resources**. There are no deny rules, so a broad `catalog.viewer` at the organization cannot be clawed back on one asset. Set the default role for new users to `catalog.none` and grant every piece of reach deliberately. Do this early, before the instance has users to reconcile.

## Scoping an agent

The shape that makes it safe to hand an AI agent a credential: its own principal, grants on the three assets it should see, nothing at the organization.

```terraform
resource "marmot_service_account" "copilot" {
  name        = "catalog-copilot"
  description = "Answers questions in #data-help. Owned by the platform team."
}

resource "marmot_asset_iam_member" "copilot_orders" {
  asset_id = marmot_asset.orders.id
  role     = "asset.viewer"
  member   = "serviceAccount:${marmot_service_account.copilot.id}"
}

resource "marmot_data_product_iam_member" "copilot_revenue" {
  data_product_id = marmot_data_product.revenue.id
  role            = "dataProduct.viewer"
  member          = "serviceAccount:${marmot_service_account.copilot.id}"
}
```

A grant on the data product is usually the better one of the two. It covers the assets in the product today and the ones a membership rule pulls in next month, without another apply.

Note that `marmot_service_account` also takes `role_ids`, which holds organization-level roles. Prefer `marmot_organization_iam_member` for those too, so every grant in the configuration reads the same way and one file answers "who can do what".

## Reviewing what it adds up to

A plan against an IAM resource is a drift report. An empty plan means nothing was granted outside the configuration since the last apply, and a non-empty one names exactly what changed.

Policy writes carry an etag, so two `terraform apply` runs against the same resource cannot silently overwrite each other; the second is rejected rather than merged.

For the question in the other direction — what does this principal actually reach — the instance has an effective-access panel on every user, team and service account, a **Check access** control on every resource, and an API behind both. A `terraform plan` tells you the configuration is intact; those tell you what it adds up to.
