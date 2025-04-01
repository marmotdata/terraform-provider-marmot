resource "marmot_lineage" "example" {
  source = marmot_asset.source.resource_id
  target = marmot_asset.target.resource_id
}

resource "marmot_asset" "source" {
  name     = "source-asset"
  type     = "dataset"
  services = ["data-service"]
}

resource "marmot_asset" "target" {
  name     = "target-asset"
  type     = "report"
  services = ["reporting-service"]
}
