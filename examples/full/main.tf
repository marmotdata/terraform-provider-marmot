terraform {
  required_providers {
    marmot = {
      source = "marmotdata/marmot"
    }
  }
}

provider "marmot" {
  host    = "http://localhost:8080"
  api_key = var.marmot_api_key
}

resource "marmot_asset" "kafka_asset" {
  name        = "customer-events-stream"
  type        = "Topic"
  description = "Kafka stream for customer events"
  services    = ["Kafka"]
  tags        = ["events", "streaming", "real-time", "customer-data"]

  metadata = {
    owner              = "platform-team"
    partitions         = "24"
    replication_factor = "3"
    retention_ms       = "604800000"
    security_protocol  = "SASL_SSL"
    compression_type   = "lz4"
    max_message_size   = "1048576"
    group_id           = "customer-events-consumers"
  }

  schema = {
    type      = "avro"
    doc       = "Schema for customer events"
    name      = "CustomerEvent"
    namespace = "com.example.events"
    fields = jsonencode([
      {
        name = "event_id"
        type = "string"
        doc  = "Unique identifier for the event"
      },
      {
        name = "customer_id"
        type = "string"
        doc  = "Customer identifier"
      },
      {
        name = "event_type"
        type = {
          type    = "enum"
          name    = "EventType"
          symbols = ["SIGN_UP", "LOGIN", "PURCHASE", "ACCOUNT_UPDATE", "LOGOUT"]
        }
      },
      {
        name = "timestamp"
        type = "long"
        doc  = "Event timestamp in milliseconds since epoch"
      },
      {
        name = "payload"
        type = {
          type = "record"
          name = "EventPayload"
          fields = [
            {
              name = "session_id"
              type = ["null", "string"]
            },
            {
              name = "device_info"
              type = {
                type = "record"
                name = "DeviceInfo"
                fields = [
                  {
                    name = "type"
                    type = ["null", "string"]
                  },
                  {
                    name = "os"
                    type = ["null", "string"]
                  },
                  {
                    name = "browser"
                    type = ["null", "string"]
                  }
                ]
              }
            },
            {
              name = "properties"
              type = {
                type   = "map"
                values = "string"
              }
            }
          ]
        }
      }
    ])
  }

  external_links = [
    {
      name = "Kafka UI"
      url  = "http://kafka-ui.example.com"
    },
    {
      name = "Monitoring"
      url  = "http://grafana.example.com/kafka-dashboard"
    },
    {
      name = "Schema Registry"
      url  = "http://schema-registry.example.com/subjects/customer-events/versions/latest"
      icon = "database-schema"
    },
    {
      name = "Documentation"
      url  = "http://docs.example.com/kafka/customer-events"
      icon = "book"
    }
  ]

  environments = {
    dev = {
      name = "Development"
      path = "dev-customer-events"
      metadata = {
        retention_ms        = "86400000"
        auto_create_topics  = "true"
        cleanup_policy      = "delete"
        min_insync_replicas = "1"
        max_message_bytes   = "1048576"
      }
    }

    test = {
      name = "Testing"
      path = "test-customer-events"
      metadata = {
        retention_ms        = "259200000"
        auto_create_topics  = "true"
        cleanup_policy      = "delete"
        min_insync_replicas = "2"
        max_message_bytes   = "1048576"
      }
    }

    prod = {
      name = "Production"
      path = "prod-customer-events"
      metadata = {
        retention_ms        = "604800000"
        auto_create_topics  = "false"
        cleanup_policy      = "compact,delete"
        min_insync_replicas = "2"
        max_message_bytes   = "1048576"
        monitoring_enabled  = "true"
      }
    }
  }
}

resource "marmot_asset" "postgres_asset" {
  name        = "customer-data-warehouse"
  type        = "Database"
  description = "PostgreSQL database for customer data"
  services    = ["PostgreSQL"]
  tags        = ["database", "warehouse", "structured-data"]

  metadata = {
    owner   = "data-team"
    version = "14.5"
    size    = "medium"
  }

  schema = {
    tables = jsonencode([
      {
        name        = "customers"
        description = "Customer records"
        columns = [
          {
            name = "id"
            type = "uuid"
          },
          {
            name = "email"
            type = "varchar(255)"
          },
          {
            name = "created_at"
            type = "timestamp"
          }
        ]
      },
      {
        name        = "orders"
        description = "Customer orders"
        columns = [
          {
            name = "id"
            type = "uuid"
          },
          {
            name = "customer_id"
            type = "uuid"
          },
          {
            name = "total"
            type = "decimal(10,2)"
          }
        ]
      }
    ])
  }

  external_links = [
    {
      name = "Database Admin"
      url  = "http://pgadmin.example.com"
    }
  ]

  environments = {
    dev = {
      name = "Development"
      path = "dev-db/customer_data"
      metadata = {
        backup_frequency = "daily"
      }
    }

    prod = {
      name = "Production"
      path = "prod-db/customer_data"
      metadata = {
        backup_frequency = "hourly"
        ha_enabled       = "true"
      }
    }
  }
}

resource "marmot_asset" "s3_asset" {
  name        = "customer-data-lake"
  type        = "Bucket"
  description = "S3 bucket for customer data lake storage"
  services    = ["S3"]
  tags        = ["storage", "data-lake", "raw-data"]

  metadata = {
    owner            = "data-platform-team"
    region           = "us-west-2"
    lifecycle_policy = "glacier-after-90-days"
  }

  external_links = [
    {
      name = "AWS Console"
      url  = "https://console.aws.amazon.com/S3/buckets/customer-data-lake"
    }
  ]

  environments = {
    dev = {
      name = "Development"
      path = "dev-customer-data-lake"
      metadata = {
        versioning = "enabled"
      }
    }

    prod = {
      name = "Production"
      path = "prod-customer-data-lake"
      metadata = {
        versioning = "enabled"
        encryption = "AES-256"
      }
    }
  }
}

resource "marmot_asset" "service_asset" {
  name        = "order-processing-service"
  type        = "Service"
  description = "Microservice for processing customer orders"
  services    = ["Kubernetes"]
  tags        = ["microservice", "orders", "processing"]

  metadata = {
    owner      = "order-team"
    language   = "golang"
    version    = "1.2.3"
    repository = "github.com/example/order-processor"
  }

  external_links = [
    {
      name = "API Documentation"
      url  = "http://docs.example.com/order-api"
    },
    {
      name = "Monitoring Dashboard"
      url  = "http://grafana.example.com/order-service"
    },
    {
      name = "Repository"
      url  = "https://github.com/example/order-processor"
      icon = "github"
    }
  ]

  environments = {
    dev = {
      name = "Development"
      path = "dev/order-processor"
      metadata = {
        replicas = "1"
      }
    }

    prod = {
      name = "Production"
      path = "prod/order-processor"
      metadata = {
        replicas    = "3"
        autoscaling = "enabled"
      }
    }
  }
}

resource "marmot_lineage" "kafka_to_service_lineage" {
  source = marmot_asset.kafka_asset.mrn
  target = marmot_asset.service_asset.mrn

  depends_on = [
    marmot_asset.kafka_asset,
    marmot_asset.service_asset
  ]
}

resource "marmot_lineage" "service_to_postgres_lineage" {
  source = marmot_asset.service_asset.mrn
  target = marmot_asset.postgres_asset.mrn

  depends_on = [
    marmot_asset.service_asset,
    marmot_asset.postgres_asset
  ]
}

resource "marmot_lineage" "service_to_s3_lineage" {
  source = marmot_asset.service_asset.mrn
  target = marmot_asset.s3_asset.mrn

  depends_on = [
    marmot_asset.service_asset,
    marmot_asset.s3_asset
  ]
}
