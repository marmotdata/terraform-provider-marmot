# IAM demo seed

Seeds a Marmot instance with a small commerce catalog and then exercises every
access-grant resource in the provider: four hierarchy targets (organization,
data product, asset, glossary term) times three authority levels (`_iam_policy`,
`_iam_binding`, `_iam_member`), plus the `marmot_iam_policy` data source.

## Running it

```sh
export MARMOT_API_KEY=...
terraform apply -var marmot_host=https://your-instance.marmotdata.cloud
```

Built against a dev override, so no `terraform init` is needed:

```sh
make install   # from the repository root
```

## The access model

The instance ships three roles: `user` (read-only), `editor` (read plus manage
assets, glossary and ingestion, and preview sample data) and `admin`.

Grants are additive and there are no denies, so the model is a thin read-only
baseline at the root and everything else granted per resource:

| Where | Resource | Grant |
| --- | --- | --- |
| organization | `_iam_binding` | `user` for all four teams: the read-only floor |
| organization | `_iam_member` | `admin` for the platform team |
| data product `orders` | `_iam_binding` | `editor` for analytics |
| data product `customer-360` | `_iam_member` | `editor` for ml, leaving other grants alone |
| data product `finance-reporting` | `_iam_policy` | authoritative: finance and the ETL account, nobody else |
| asset `finance.revenue_daily` | `_iam_binding` | `editor` for finance and the ETL account |
| asset `commerce.orders.events` | `_iam_member` | `user` for the copilot service account |
| asset `product-catalog-api` | `_iam_policy` | authoritative: `allAuthenticated` reads, platform edits |
| term `Customer` | `_iam_binding` | `editor` for analytics and ml, inherited by child terms |
| term `Gross Merchandise Value` | `_iam_member` | `user` for the copilot service account |
| term `Recognised Revenue` | `_iam_policy` | authoritative: finance edits, analytics reads |

## Things to show

- **A principal with no organization role at all.** `catalog-copilot` holds no
  role at the root. Its entire reach is two `_iam_member` grants: one topic and
  one glossary term. Delete either and it loses that, and only that.
- **Authoritative versus not.** Add a member to `finance-reporting` in the UI,
  then re-plan: the `_iam_policy` resource removes it. Do the same on
  `customer-360`, whose grant is an `_iam_member`, and the plan stays empty.
- **Inheritance.** The grant on `Customer` is what gives analytics and ml edit
  access to `Active Customer` and `Churned Customer`, neither of which is named
  in any grant.
- **Etags.** `terraform output policy_etags` shows the version each policy was
  last read at. Change a policy out of band mid-apply and the write is rejected
  rather than silently overwriting the other writer.
- **Restriction without denies.** Nothing grants `editor` at the root, so no
  team can edit outside what it was granted, even though every team can read
  everything.
