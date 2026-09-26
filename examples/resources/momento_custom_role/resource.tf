# Manage a Momento custom role.
resource "momento_custom_role" "example" {
  name        = "custom-role-name"
  description = "This is my sample Momento custom role created with Terraform."
  permissions = {
    rules = [
      {
        type        = "account_management",
        permissions = ["read", "list"]
      },
      {
        type        = "cache",
        permissions = ["read", "list"],
        caches      = { all = true },
        items       = { key_prefix = "foo-" }
      },
      {
        type        = "function",
        permissions = ["invoke"],
        caches      = { name = "edge-app" },
        functions   = { prefix = "webhook-" }
      }
    ],
    conditions = [
      {
        ip_filter = {
          allowed_cidr_ranges = [
            "10.0.0.0/8",
            "192.168.1.0/24",
            "2001:db8::/32"
          ]
        }
      }
    ]
  }
}
