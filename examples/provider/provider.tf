# Select only one of the following configuration methods (or their environment variable equivalents).
# Do not provide both legacy and non-legacy configurations.

# Configuration-based authentication using legacy API key
provider "momento" {
  api_key = "my-legacy-momento-api-key"
}

# Configuration-based authentication using Momento API key and service hostname (using sdk endpoint format).
# For more information: https://docs.momentohq.com/platform/regions#resp-and-sdk-endpoints
provider "momento" {
  v2_api_key      = "my-momento-api-key"
  v2_api_endpoint = "cell-1-ap-southeast-1-1.prod.a.momentohq.com"
}
