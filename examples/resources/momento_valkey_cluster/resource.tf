# Creates a small test cluster in us-west-2 region with all optional configs (description and shard_placements) specified.
resource "momento_valkey_cluster" "example" {
  cluster_name           = "cluster-name"
  description            = "momento-managed valkey cluster"
  enforce_shard_multi_az = false
  node_instance_type     = "cache.t3.micro"
  replication_factor     = 1
  shard_count            = 1
  shard_placements = [{
    index                      = 0
    availability_zone          = "us-west-2a"
    replica_availability_zones = ["us-west-2b"]
  }]
}