terraform {
  required_version = ">= 1.11"

  required_providers {
    marmot = {
      source = "marmotdata/marmot"
    }
  }
}

# api_key comes from MARMOT_API_KEY.
provider "marmot" {
  host = var.marmot_host
}
