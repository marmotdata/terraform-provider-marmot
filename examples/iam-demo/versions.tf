terraform {
  required_version = ">= 1.11"

  required_providers {
    marmot = {
      source = "marmotdata/marmot"
    }
  }
}

# api_key comes from MARMOT_API_KEY so the credential never reaches a file.
provider "marmot" {
  host = var.marmot_host
}
