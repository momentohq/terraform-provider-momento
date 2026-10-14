resource "momento_capacity_pool" "example_flex" {
  timeouts { create = "3m" }

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
      family = "standard",
      zones = [
        "usw2-az1",
        "usw2-az2"
      ]
    }
  }
}

resource "momento_capacity_pool" "example_cluster" {
  timeouts { create = "3m" }

  name = "cluster-pool-name"
  provisioning = {
    cluster = {
      instance_type      = "t4g.medium",
      shard_count        = 1,
      replicas_per_shard = 0,
      zones = [
        "usw2-az1",
        "usw2-az2"
      ]
    }
  }
}
