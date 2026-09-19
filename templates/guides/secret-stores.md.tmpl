---
page_title: "Secret stores"
subcategory: "Guides"
description: |-
  Register where a secret lives in your vault, let a pipeline read it just before each run, and keep the value out of Marmot and out of Terraform state.
---

# Secret stores

~> **Requires Marmot Cloud or Marmot Enterprise.** Open source Marmot has no secret-store API, so these resources fail on apply.

Some sources cannot federate. A PostgreSQL database behind a password has no cloud identity to present, and the password has to come from somewhere.

A secret store is that somewhere: a pointer to your vault. **Marmot never holds a secret value.** A store says which vault and how to authenticate to it, a secret resource says where in that vault a value lives, and a pipeline names the secret it needs. Marmot reads the value just before each run and it never reaches Terraform state.

| Resource | Says |
| --- | --- |
| `marmot_secret_store_google` | Which vault, and how Marmot authenticates to it. |
| `marmot_secret_store_google_secret` | Where in that vault the value lives. |
| `marmot_pipeline.secrets` | Which config key to inject it at. |

## A store per environment

Four backends are supported, each with a matching secret resource:

| Store | Secret resource | Federates with |
| --- | --- | --- |
| `marmot_secret_store_google` | `marmot_secret_store_google_secret` | `workload_identity_provider` |
| `marmot_secret_store_aws` | `marmot_secret_store_aws_secret` | `role_arn` |
| `marmot_secret_store_azure` | `marmot_secret_store_azure_secret` | `tenant_id` + `client_id` |
| `marmot_secret_store_vault` | `marmot_secret_store_vault_secret` | `role` on a JWT auth mount |

Leave the federation field out and the store reads with the server's own ambient credentials, which works but makes every read look the same in your vault's audit log. Set it and the store presents an OIDC token with the subject `secretStore:{name}`, so your vault records which store asked.

Federate. It is a few more lines and it is the difference between an audit log that names the caller and one that does not.

## Google Secret Manager

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

resource "marmot_secret_store_google" "prod" {
  name                       = "gcp-prod"
  workload_identity_provider = google_iam_workload_identity_pool_provider.marmot.name
}

resource "marmot_secret_store_google_secret" "orders_db_password" {
  store     = marmot_secret_store_google.prod.id
  project   = "acme-prod"
  secret_id = "orders-db-password"
}

resource "google_secret_manager_secret_iam_member" "marmot_orders_db" {
  secret_id = google_secret_manager_secret.orders_db_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.marmot.name}/subject/${marmot_secret_store_google.prod.subject}"
}
```

Grant the accessor role **per secret**, not on the project. A store scoped to the three secrets Marmot needs cannot read the fourth, and that is the whole point of pointing at a vault rather than copying values into one.

## AWS Secrets Manager

```terraform
resource "marmot_secret_store_aws" "prod" {
  name     = "aws-prod"
  role_arn = aws_iam_role.marmot_secrets.arn
}

resource "marmot_secret_store_aws_secret" "orders_db_password" {
  store     = marmot_secret_store_aws.prod.id
  secret_id = aws_secretsmanager_secret.orders_db_password.arn
}
```

The role's trust policy pins `secretStore:aws-prod` as the subject, the same shape as a pipeline's. Passing an ARN as `secret_id` means `region` can be left out.

## HashiCorp Vault

```terraform
resource "marmot_secret_store_vault" "prod" {
  name    = "vault-prod"
  address = "https://vault.acme.internal"
  role    = "marmot"
}

resource "marmot_secret_store_vault_secret" "orders_db_password" {
  store = marmot_secret_store_vault.prod.id
  mount = "secret"
  name  = "databases/orders"
  key   = "password"
}
```

The JWT auth role on the Vault side binds the issuer, the subject `secretStore:vault-prod` and the audience the store reports, and its policy should allow reading exactly the paths Marmot needs.

## Inject a secret into a pipeline

`secrets` maps a key inside `config` to a secret resource. Leave that key out of `config` entirely. Marmot writes it in before each run.

```terraform
resource "marmot_pipeline" "orders" {
  name      = "orders"
  plugin_id = "postgresql"

  config = jsonencode({
    host     = "orders-db.acme.internal"
    database = "orders"
    user     = "marmot"
  })

  secrets = {
    password = marmot_secret_store_google_secret.orders_db_password.id
  }

  cron_expression = "0 * * * *"
}
```

The key is a dot path, so a nested field works too:

```terraform
resource "marmot_pipeline" "analytics" {
  name      = "analytics"
  plugin_id = "bigquery"

  config = jsonencode({
    project_id = "acme-analytics-prod"
  })

  secrets = {
    "credentials.private_key" = marmot_secret_store_google_secret.bq_key.id
  }

  cron_expression = "0 */6 * * *"
}
```

Only the reference is stored. Rotate the value in your vault and the next run picks it up with no apply, which is the real benefit: rotation stops being a Terraform change.

## Who can use a store

A store is an IAM resource with three roles that matter:

| Role | Can |
| --- | --- |
| `secretStore.viewer` | See the store and which secrets are registered. Not the values. |
| `secretStore.user` | Register secrets and attach them to pipelines. Not the values. |
| `secretStore.reader` | Read the secret values. |

```terraform
resource "marmot_secret_store_iam_member" "platform_uses_prod" {
  secret_store_id = marmot_secret_store_google.prod.id
  role            = "secretStore.user"
  member          = "group:${marmot_team.platform.id}"
}
```

Whoever writes pipelines needs `secretStore.user`. They can point a pipeline at `orders-db-password` and never see what it is. `secretStore.reader` is for the rare case where a human has to read a value back, and it should be granted to people rather than to a team by default.

This separation is what makes a secret store better than a variable. A password in `var.orders_db_password` is visible to everyone who can run `terraform plan`; a secret reference is visible to everyone, and the value to nobody.

## Never put a value in the configuration

Marmot has no resource that writes a secret value, by design. If a password has to exist before a pipeline can read it, create it with your cloud's own provider and a write-only argument, so it never lands in that provider's state either:

```terraform
resource "google_secret_manager_secret_version" "orders_db_password" {
  secret                 = google_secret_manager_secret.orders_db_password.id
  secret_data_wo         = var.orders_db_password_wo
  secret_data_wo_version = 1
}
```

Then register it with `marmot_secret_store_google_secret` and forget about it.

## Troubleshooting

| Symptom | Usually |
| --- | --- |
| `issuer` and `subject` are null | The store is not federating: the federation field is unset, or the server has no workload identity issuer. |
| The vault refuses the token | Issuer, subject or audience differs from what the store reports. Read them from the resource rather than writing them by hand. |
| The pipeline runs but the credential is wrong | The `secrets` key does not match the config key the plugin expects. Check the plugin's page on the [plugin registry](https://plugins.marmotdata.io). |
| Terraform cannot attach a secret | The principal needs `secretStore.user` on the store. |
