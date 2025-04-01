resource "marmot_asset" "example" {
  name        = "example-asset"
  type        = "Database"
  description = "An example dataset asset"
  services    = ["PostgreSQL"]

  tags = ["example", "terraform"]

  metadata = {
    "owner"      = "data-team"
    "department" = "engineering"
  }

  external_links {
    name = "Documentation"
    url  = "https://example.com/docs"
    icon = "doc"
  }

  sources {
    name     = "source1"
    priority = 1
    properties = {
      "connection" = "jdbc:postgresql://localhost:5432/db"
    }
  }

  environments = {
    "prod" = {
      name = "Production"
      path = "/data/prod"
      metadata = {
        "region" = "us-west"
      }
    }
    "dev" = {
      name = "Development"
      path = "/data/dev"
    }
  }
}
