resource "momento_valkey_cluster" "example" {
  cluster_name           = "cluster-name"
  enforce_shard_multi_az = false
  node_instance_type     = "cache.t3.micro"
  replication_factor     = 1
  shard_count            = 1

  # Updates can take an especially long time, configure as needed
  timeouts {
    create = "20m"
    update = "60m"
    delete = "20m"
  }
}

# Creates a Momento object store in us-west-2 region with all optional configs
# (s3_prefix, access_logging_config, metrics_config, and throttling_limits) specified.
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
  throttling_limits = {
    read_operations_per_second  = 1000
    write_operations_per_second = 500
    read_bytes_per_second       = 10485760 # 10 MB/s
    write_bytes_per_second      = 5242880  # 5 MB/s
  }
  # Explicit dependency: Forces object store creation to wait for cluster creation
  depends_on = [
    momento_valkey_cluster.example,
  ]
}