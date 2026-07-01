output "kafka_asset_id" {
  value = marmot_asset.kafka_asset.id
}

output "kafka_asset_mrn" {
  value = marmot_asset.kafka_asset.mrn
}

output "postgres_asset_id" {
  value = marmot_asset.postgres_asset.id
}

output "postgres_asset_mrn" {
  value = marmot_asset.postgres_asset.mrn
}

output "s3_asset_id" {
  value = marmot_asset.s3_asset.id
}

output "s3_asset_mrn" {
  value = marmot_asset.s3_asset.mrn
}

output "service_asset_id" {
  value = marmot_asset.service_asset.id
}

output "service_asset_mrn" {
  value = marmot_asset.service_asset.mrn
}

output "kafka_to_service_lineage_id" {
  value = marmot_lineage.kafka_to_service_lineage.id
}

output "service_to_postgres_lineage_id" {
  value = marmot_lineage.service_to_postgres_lineage.id
}

output "service_to_s3_lineage_id" {
  value = marmot_lineage.service_to_s3_lineage.id
}
