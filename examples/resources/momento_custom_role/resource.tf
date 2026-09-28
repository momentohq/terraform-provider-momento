# Manage a Momento custom role. Requires a V2 API key (and V2 endpoint).
# Full permission set example on https://docs.momentohq.com/platform/authentication/roles-http-api#full-permission-set-example:
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
        type        = "auth_management",
        permissions = ["read", "write", "list"],
      items = { all = true } },
      {
        type        = "resource_management",
        permissions = ["read", "write", "list"],
      resources = { all = true } },
      {
        type        = "database",
        permissions = ["read", "write"],
        databases   = { all = true },
      items = { all = true } },
      {
        type        = "database",
        permissions = ["read"],
        databases   = { name = "orders" },
      items = { key_prefix = "orders:2026-" } },
      {
        type        = "database",
        permissions = ["read"],
        databases   = { name = "orders" },
      items = { key = "orders:pending" } },
      {
        type        = "cache",
        permissions = ["read", "write", "list"],
        caches      = { "all" : true },
      items = { all = true } },
      {
        type        = "cache",
        permissions = ["read"],
        caches      = { name = "prod-cache" },
      items = { key_prefix = "public/" } },
      {
        type        = "cache",
        permissions = ["write"],
        caches      = { name = "prod-cache" },
      items = { key = "feature-flags" } },
      {
        type        = "topic",
        permissions = ["read", "write", "list"],
        caches      = { all = true },
      topics = { all = true } },
      {
        type        = "topic",
        permissions = ["read"],
        caches      = { name = "chat-app" },
      topics = { name = "announcements" } },
      {
        type        = "topic",
        permissions = ["write"],
        caches      = { name = "chat-app" },
      topics = { prefix = "room-" } },
      {
        type        = "store",
        permissions = ["read", "write", "list"],
        stores      = { all = true },
      items = { all = true } },
      {
        type        = "store",
        permissions = ["read"],
        stores      = { name = "user-prefs" },
      items = { key_prefix = "org:42:" } },
      {
        type        = "store",
        permissions = ["write"],
        stores      = { name = "user-prefs" },
      items = { key = "schema-version" } },
      {
        type        = "function",
        permissions = ["invoke"],
        caches      = { all = true },
      functions = { all = true } },
      {
        type        = "function",
        permissions = ["invoke"],
        caches      = { name = "edge-app" },
      functions = { name = "resize-image" } },
      {
        type        = "function",
        permissions = ["invoke"],
        caches      = { name = "edge-app" },
      functions = { prefix = "webhook-" } }
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
