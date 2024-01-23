provider "momento" {
  # example configuration here
}

resource "momento_cache" "example" {
  name = "test-cache3"
}

data "momento_caches" "all" {}

output "all_caches" {
  value = data.momento_caches.all.caches
}
