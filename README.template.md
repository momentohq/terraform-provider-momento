
{{ ossHeader }}

# Momento Terraform Provider

The official Momento Terraform provider to manage [Momento](https://www.gomomento.com/) resources.

Full documentation for the provider can be found on the Terraform registry [here](https://registry.terraform.io/providers/Chriscbr/momento/latest/docs).

Originally authored by [Chriscbr](https://github.com/Chriscbr).

## Usage

```hcl
terraform {
  required_providers {
    momento = {
      source = "Chriscbr/momento"
    }
  }
}

provider "momento" {
  auth_token = var.auth_token
}
```

The provider can use an authentication token (API key) from Momento.
It can be provided through the configuration block, or through the `MOMENTO_AUTH_TOKEN` environment variable.

### Creating a cache

```hcl
resource "momento_cache" "example" {
  name = "example"
}
```

{{ ossFooter }}
