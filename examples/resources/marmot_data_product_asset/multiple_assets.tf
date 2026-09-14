resource "marmot_data_product_asset" "orders" {
  for_each = {
    orders   = marmot_asset.orders.id
    payments = marmot_asset.payments.id
    refunds  = marmot_asset.refunds.id
  }

  data_product_id = marmot_data_product.orders.id
  asset_id        = each.value
}
