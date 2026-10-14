resource "momento_valkey_database" "example" {
  name      = "database-name"
  pool_name = momento_capacity_pool.example.name
  metrics_config = {
    cloud_watch = {
      iam_role_arn = local.metrics_iam_role
      region       = "us-east-1"
    }
  }
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

locals {
  metrics_iam_role = "arn:aws:iam::123456789012:role/MomentoMetricsRole"
}
