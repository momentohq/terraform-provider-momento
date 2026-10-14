resource "momento_valkey_database" "example" {
  name      = "database-name"
  pool_name = momento_capacity_pool.example.name
}

resource "momento_capacity_pool" "example" {
  name = "flex-pool-name"
  provisioning = {
    flex = {
      capacity = {
        max_gib = 25,
        min_gib = 20
      },
      replication = {
        max_replicas_per_shard = 1,
        min_replicas_per_shard = 0
      },
      zones = [
        "usw2-az1",
        "usw2-az2"
      ]
    }
  }
}
