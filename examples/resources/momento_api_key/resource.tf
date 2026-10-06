# Manage a Momento API key. Requires a V2 API key (and V2 endpoint).
resource "momento_api_key" "example" {
  lifecycle {
    # If a new/updated API key fails to create, keep the old one:
    create_before_destroy = true
  }
  description = "This is my sample Momento API key created with Terraform."
  role_id     = "r-viewer"
  expiry      = time_offset.key_expiry.rfc3339
}

resource "time_offset" "key_expiry" {
  offset_hours = 6
}

output "key_id" {
  description = "ID of my generated API key."
  value       = momento_api_key.example.key_id
}

# You can access the generated API key and its refresh token
# (e.g. to pass them to your secrets manager resource)
# via momento_api_key.example.api_key and momento_api_key.example.refresh_token
