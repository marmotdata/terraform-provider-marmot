resource "marmot_service_account" "etl" {
  name        = "etl"
  description = "Runs the nightly warehouse load"
}
