resource "marmot_lineage" "example" {
  source = marmot_asset.source.mrn
  target = marmot_asset.target.mrn
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
