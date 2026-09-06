variable "marmot_host" {
  description = "Marmot API host URL."
  type        = string
  default     = "https://mni9yr33ngi.marmotdata.cloud"
}

variable "demo_password" {
  description = "Initial password for the demo users. Write-only, so it never lands in state."
  type        = string
  sensitive   = true
  default     = "MarmotDemo!2026"
}
