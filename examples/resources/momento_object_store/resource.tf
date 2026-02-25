resource "momento_valkey_cluster" "example" {
  cluster_name           = "cluster-name"
  description            = "momento-managed valkey cluster"
  enforce_shard_multi_az = false
  node_instance_type     = "cache.t3.micro"
  replication_factor     = 1
  shard_count            = 1
}

# Creates a Momento object store in us-west-2 region with all optional configs (s3_prefix, access_logging_config, and metrics_config) specified.
# Waits for the valkey cluster to be created first if creating both for the first time.
resource "momento_object_store" "example" {
  name                = "object-store-name"
  s3_bucket_name      = "s3-bucket-name"
  s3_iam_role_arn     = "s3-iam-role-arn"
  s3_prefix           = "prefix"
  valkey_cluster_name = "cluster-name"
  access_logging_config = {
    iam_role_arn   = "cloudwatch-iam-role-arn"
    log_group_name = "log-group-name"
    region         = "us-west-2"
  }
  metrics_config = {
    iam_role_arn = "metrics-iam-role-arn"
    region       = "us-west-2"
  }
  # Explicit dependency: Forces object store creation to wait for cluster creation
  depends_on = [
    momento_valkey_cluster.example,
  ]
}