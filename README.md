# Terraform Provider: Marmot

The [Marmot Terraform provider](https://registry.terraform.io/providers/marmotdata/marmot/0.3.0/docs)
lets you manage your [Marmot](https://marmotdata.io) instance as code. It populates
Marmot from Terraform, letting you declare your Marmot resources alongside the
infrastructure they describe.

* [Terraform Registry](https://registry.terraform.io/providers/marmotdata/marmot/0.3.0/docs)
* [Marmot documentation](https://marmotdata.io/docs)

## Usage

To use the provider, declare it as a required provider in your Terraform configuration:

```hcl
terraform {
  required_providers {
    marmot = {
      source = "marmotdata/marmot"
    }
  }
}
```

The provider authenticates with a Marmot API key, set through the `api_key`
attribute or the `MARMOT_API_KEY` environment variable. A bearer `token` (or `MARMOT_TOKEN`) is also
supported, and when no credential is provided the provider falls back to the Marmot
CLI credentials from `marmot login`.

To keep the secret entirely out of state, you can inject it using a Terraform
[ephemeral resource](https://developer.hashicorp.com/terraform/language/resources/ephemeral). 

For example, with Google Secret Manager:

```hcl
ephemeral "google_secret_manager_secret_version" "marmot_api_key" {
  secret  = "marmot-api-key"
  version = "latest"
}

provider "marmot" {
  host    = "https://your-marmot-host.com"
  api_key = ephemeral.google_secret_manager_secret_version.marmot_api_key.secret_data
}
```

The same pattern works with any provider that exposes secrets as an ephemeral
resource, such as AWS Secrets Manager or HashiCorp Vault.


## Assets

Register the datasets, services, and other resources in your platform as assets:

```hcl
resource "marmot_asset" "customer_orders" {
  name        = "customer-orders"
  type        = "Database"
  services    = ["PostgreSQL"]
  tags        = ["orders", "customer", "customer-orders"]
}
```

## Lineage

Describes how data flows between assets to build a lineage graph:

```hcl
resource "marmot_asset" "order_processor" {
  name     = "order-processor"
  type     = "Service"
  services = ["Kubernetes"]
}

resource "marmot_lineage" "orders_to_processor" {
  source = marmot_asset.customer_orders.mrn
  target = marmot_asset.order_processor.mrn
}
```

## Glossary Terms

Define shared business terminology and organize it hierarchically:

```hcl
resource "marmot_glossary_term" "active_customer" {
  name       = "Active Customer"
  definition = "A customer with at least one order in the last 90 days."
  metadata = {
    domain = "sales"
  }
}
```

## Teams and Users

Manage the teams and users that own catalog entities. A user's password goes
through the write-only `password_wo` attribute (Terraform >= 1.11), so it never
lands in state:

```hcl
resource "marmot_team" "analytics" {
  name = "analytics"
}

ephemeral "random_password" "alice" {
  length = 24
}

resource "marmot_user" "alice" {
  name                = "Alice Nguyen"
  username            = "alice"
  password_wo         = ephemeral.random_password.alice.result
  password_wo_version = "1"
}
```

## Data Products

Group related assets into a data product. Add assets directly, or match them
with a rule:

```hcl
resource "marmot_data_product" "orders" {
  name        = "orders"
  description = "Order events and the tables derived from them"
  tags        = ["orders"]

  owner_team_ids = [marmot_team.analytics.id]
}

resource "marmot_data_product_asset" "orders_table" {
  data_product_id = marmot_data_product.orders.id
  asset_id        = marmot_asset.orders_table.id
}

resource "marmot_data_product_rule" "order_datasets" {
  data_product_id = marmot_data_product.orders.id

  name             = "order-datasets"
  type             = "query"
  query_expression = "tag:orders"
}
```

## Requirements

* [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
  (>= 1.10 to inject credentials from an ephemeral resource)
* [Go](https://go.dev/doc/install) >= 1.25 (to build the provider from source)

## Developing the Provider

To build and install the provider into your `$GOPATH/bin`:

```shell
go install
```

To generate or update the documentation under `docs/`:

```shell
make generate
```

To run the acceptance tests (these create real resources against a Marmot instance and
may incur cost):

```shell
make testacc
```

## Contributing

Contributions are welcome! Whether it's a bug report, a feature request, or a pull
request, thank you for investing your time in the project. Please open an issue to
discuss significant changes before starting work, and make sure `go build ./...`,
`go vet ./...`, and the linter pass before submitting.

## License

[Mozilla Public License v2.0](./LICENSE)
