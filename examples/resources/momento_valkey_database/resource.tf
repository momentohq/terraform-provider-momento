# First create a Capacity Pool via Terraform, the Momento CLI (`momento pool create -n pool-name`), or the Momento Console.
# When the Pool has finished creating (`momento pool get-status -n pool-name`), you can create a Valkey Database:
resource "momento_valkey_database" "example" {
  name      = "database-name"
  pool_name = "pool-name"
}
