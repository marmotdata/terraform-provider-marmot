# The catalog: an event-driven commerce platform, from the topics the storefront
# emits through to the dashboards and models built on top of them.

resource "marmot_asset" "orders_events" {
  name        = "commerce.orders.events"
  type        = "Topic"
  description = "Every order lifecycle event emitted by the storefront"
  services    = ["Kafka"]
  tags        = ["orders", "events", "streaming", "domain:commerce"]

  metadata = {
    domain             = "commerce"
    owner              = "platform"
    partitions         = "24"
    replication_factor = "3"
    retention_ms       = "604800000"
    cleanup_policy     = "delete"
    security_protocol  = "SASL_SSL"
    contains_pii       = "false"
  }

  schema = {
    type      = "avro"
    name      = "OrderEvent"
    namespace = "com.acme.commerce"
    doc       = "Order lifecycle event"
    fields = jsonencode([
      { name = "order_id", type = "string", doc = "Order identifier" },
      { name = "customer_id", type = "string", doc = "Customer identifier" },
      {
        name = "status"
        type = {
          type    = "enum"
          name    = "OrderStatus"
          symbols = ["CREATED", "PAID", "SHIPPED", "DELIVERED", "CANCELLED", "REFUNDED"]
        }
      },
      { name = "total_minor_units", type = "long", doc = "Order total in minor currency units" },
      { name = "currency", type = "string" },
      { name = "occurred_at", type = "long", doc = "Milliseconds since epoch" },
    ])
  }

  external_links = [
    {
      name = "Schema Registry"
      url  = "https://schema-registry.acme.internal/subjects/commerce.orders.events/versions/latest"
      icon = "database-schema"
    },
    {
      name = "Runbook"
      url  = "https://docs.acme.internal/runbooks/orders-topic"
      icon = "book"
    },
  ]

  sources = [{
    name     = "kafka"
    priority = 1
    properties = {
      cluster   = "commerce-prod"
      bootstrap = "b-1.commerce.acme.internal:9096"
    }
  }]

  environments = {
    dev = {
      name = "Development"
      path = "dev.commerce.orders.events"
      metadata = {
        retention_ms        = "86400000"
        min_insync_replicas = "1"
      }
    }
    prod = {
      name = "Production"
      path = "prod.commerce.orders.events"
      metadata = {
        retention_ms        = "604800000"
        min_insync_replicas = "2"
        monitoring_enabled  = "true"
      }
    }
  }
}

resource "marmot_asset" "payments_events" {
  name        = "commerce.payments.events"
  type        = "Topic"
  description = "Authorisations, captures and refunds from the payment gateway"
  services    = ["Kafka"]
  tags        = ["payments", "events", "streaming", "restricted", "domain:finance"]

  metadata = {
    domain              = "finance"
    owner               = "platform"
    partitions          = "12"
    replication_factor  = "3"
    contains_pii        = "true"
    classification      = "restricted"
    pci_scope           = "true"
    retention_ms        = "2592000000"
    encryption_at_rest  = "true"
    field_level_masking = "card_bin,card_last4"
  }

  schema = {
    type      = "avro"
    name      = "PaymentEvent"
    namespace = "com.acme.finance"
    fields = jsonencode([
      { name = "payment_id", type = "string" },
      { name = "order_id", type = "string" },
      { name = "amount_minor_units", type = "long" },
      { name = "currency", type = "string" },
      { name = "card_bin", type = ["null", "string"], doc = "Masked outside the finance domain" },
      { name = "outcome", type = "string" },
    ])
  }

  environments = {
    prod = {
      name = "Production"
      path = "prod.commerce.payments.events"
      metadata = {
        min_insync_replicas = "2"
        audit_logging       = "enabled"
      }
    }
  }
}

resource "marmot_asset" "raw_events_lake" {
  name        = "acme-raw-events"
  type        = "Bucket"
  description = "Landing zone for every raw event stream, partitioned by day"
  services    = ["S3"]
  tags        = ["storage", "data-lake", "raw", "domain:platform"]

  metadata = {
    domain           = "platform"
    owner            = "platform"
    region           = "eu-west-1"
    storage_class    = "INTELLIGENT_TIERING"
    lifecycle_policy = "glacier-after-90-days"
    versioning       = "enabled"
    encryption       = "aws:kms"
  }

  external_links = [{
    name = "AWS Console"
    url  = "https://eu-west-1.console.aws.amazon.com/s3/buckets/acme-raw-events"
  }]

  environments = {
    prod = {
      name = "Production"
      path = "s3://acme-raw-events/prod"
      metadata = {
        object_count_estimate = "412000000"
      }
    }
  }
}

resource "marmot_asset" "order_processor" {
  name        = "order-processor"
  type        = "Service"
  description = "Consumes order events, applies the state machine, writes to the OLTP store"
  services    = ["Kubernetes"]
  tags        = ["microservice", "orders", "domain:commerce"]

  metadata = {
    domain     = "commerce"
    owner      = "platform"
    language   = "go"
    version    = "4.11.2"
    repository = "github.com/acme/order-processor"
    slo_target = "99.9"
    namespace  = "commerce"
  }

  external_links = [
    {
      name = "Repository"
      url  = "https://github.com/acme/order-processor"
      icon = "github"
    },
    {
      name = "Dashboard"
      url  = "https://grafana.acme.internal/d/order-processor"
    },
  ]

  environments = {
    prod = {
      name = "Production"
      path = "commerce/order-processor"
      metadata = {
        replicas    = "6"
        autoscaling = "enabled"
      }
    }
  }
}

resource "marmot_asset" "payments_reconciler" {
  name        = "payments-reconciler"
  type        = "Service"
  description = "Matches gateway settlements against captured payments nightly"
  services    = ["Kubernetes"]
  tags        = ["microservice", "payments", "restricted", "domain:finance"]

  metadata = {
    domain         = "finance"
    owner          = "finance"
    language       = "python"
    version        = "2.3.0"
    repository     = "github.com/acme/payments-reconciler"
    classification = "restricted"
    schedule       = "0 2 * * *"
  }
}

resource "marmot_asset" "commerce_oltp" {
  name        = "commerce-oltp"
  type        = "Database"
  description = "Transactional Postgres behind the storefront"
  services    = ["PostgreSQL"]
  tags        = ["database", "oltp", "domain:commerce"]

  metadata = {
    domain       = "commerce"
    owner        = "platform"
    version      = "16.3"
    instance     = "db.r6g.4xlarge"
    contains_pii = "true"
    ha_enabled   = "true"
  }

  schema = {
    tables = jsonencode([
      {
        name        = "orders"
        description = "One row per order"
        columns = [
          { name = "id", type = "uuid" },
          { name = "customer_id", type = "uuid" },
          { name = "status", type = "text" },
          { name = "total_minor_units", type = "bigint" },
          { name = "created_at", type = "timestamptz" },
        ]
      },
      {
        name        = "customers"
        description = "Customer records"
        columns = [
          { name = "id", type = "uuid" },
          { name = "email", type = "citext" },
          { name = "country", type = "char(2)" },
          { name = "created_at", type = "timestamptz" },
        ]
      },
    ])
  }

  environments = {
    prod = {
      name = "Production"
      path = "prod-commerce/commerce"
      metadata = {
        backup_frequency = "hourly"
        pitr_window_days = "7"
      }
    }
  }
}

resource "marmot_asset" "orders_fact" {
  name        = "analytics.orders_fact"
  type        = "Table"
  description = "One row per order, conformed and deduplicated"
  services    = ["Snowflake", "dbt"]
  tags        = ["warehouse", "fact", "orders", "domain:commerce"]

  metadata = {
    domain          = "commerce"
    owner           = "analytics"
    materialisation = "incremental"
    grain           = "one row per order"
    row_count       = "184300000"
    dbt_model       = "models/marts/orders_fact.sql"
    refresh_cron    = "0 * * * *"
  }

  schema = {
    columns = jsonencode([
      { name = "order_id", type = "varchar", description = "Natural key" },
      { name = "customer_key", type = "varchar" },
      { name = "order_status", type = "varchar" },
      { name = "gross_merchandise_value", type = "number(18,2)" },
      { name = "ordered_at", type = "timestamp_ntz" },
    ])
  }

  external_links = [{
    name = "dbt docs"
    url  = "https://dbt.acme.internal/#!/model/model.acme.orders_fact"
  }]
}

resource "marmot_asset" "customers_dim" {
  name        = "analytics.customers_dim"
  type        = "Table"
  description = "Slowly changing customer dimension, type 2"
  services    = ["Snowflake", "dbt"]
  tags        = ["warehouse", "dimension", "customer", "domain:commerce"]

  metadata = {
    domain          = "commerce"
    owner           = "analytics"
    materialisation = "table"
    scd_type        = "2"
    contains_pii    = "true"
    row_count       = "9100000"
    dbt_model       = "models/marts/customers_dim.sql"
  }
}

resource "marmot_asset" "revenue_daily" {
  name        = "finance.revenue_daily"
  type        = "Table"
  description = "Recognised revenue by day and region, the source for the board pack"
  services    = ["Snowflake", "dbt"]
  tags        = ["warehouse", "finance", "restricted", "domain:finance"]

  metadata = {
    domain           = "finance"
    owner            = "finance"
    classification   = "restricted"
    materialisation  = "table"
    grain            = "one row per day per region"
    accounting_basis = "ASC 606"
    sox_relevant     = "true"
    refresh_cron     = "30 3 * * *"
  }

  schema = {
    columns = jsonencode([
      { name = "revenue_date", type = "date" },
      { name = "region", type = "varchar" },
      { name = "recognised_revenue", type = "number(18,2)" },
      { name = "deferred_revenue", type = "number(18,2)" },
      { name = "refunds", type = "number(18,2)" },
    ])
  }
}

resource "marmot_asset" "exec_revenue_dashboard" {
  name        = "Executive Revenue"
  type        = "Dashboard"
  description = "Board-level revenue, margin and refund trend"
  services    = ["Looker"]
  tags        = ["dashboard", "finance", "restricted", "domain:finance"]

  metadata = {
    domain         = "finance"
    owner          = "finance"
    classification = "restricted"
    audience       = "executive"
    refresh        = "daily"
  }

  external_links = [{
    name = "Open in Looker"
    url  = "https://looker.acme.internal/dashboards/executive-revenue"
  }]
}

resource "marmot_asset" "churn_model" {
  name        = "churn-propensity"
  type        = "Model"
  description = "Gradient boosted model scoring 90-day churn propensity"
  services    = ["MLflow"]
  tags        = ["model", "churn", "domain:ml"]

  metadata = {
    domain           = "ml"
    owner            = "ml"
    framework        = "xgboost"
    version          = "7"
    training_cadence = "weekly"
    auc              = "0.83"
    feature_count    = "142"
  }

  external_links = [{
    name = "MLflow run"
    url  = "https://mlflow.acme.internal/#/models/churn-propensity/versions/7"
  }]
}

resource "marmot_asset" "catalog_api" {
  name        = "product-catalog-api"
  type        = "API"
  description = "Public REST API serving the product catalog to storefront clients"
  services    = ["Kong"]
  tags        = ["api", "public", "domain:commerce"]

  metadata = {
    domain       = "commerce"
    owner        = "platform"
    protocol     = "REST"
    auth         = "oauth2"
    rate_limit   = "1000/min"
    openapi_spec = "https://api.acme.com/catalog/openapi.json"
  }

  external_links = [{
    name = "OpenAPI"
    url  = "https://api.acme.com/catalog/openapi.json"
    icon = "doc"
  }]
}
