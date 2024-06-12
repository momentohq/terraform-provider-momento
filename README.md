
<img src="https://docs.momentohq.com/img/momento-logo-forest.svg" alt="logo" width="400"/>

[![project status](https://momentohq.github.io/standards-and-practices/badges/project-status-official.svg)](https://github.com/momentohq/standards-and-practices/blob/main/docs/momento-on-github.md)
[![project stability](https://momentohq.github.io/standards-and-practices/badges/project-stability-alpha.svg)](https://github.com/momentohq/standards-and-practices/blob/main/docs/momento-on-github.md)


# Momento Terraform Provider

The official Momento Terraform provider to manage [Momento](https://www.gomomento.com/) resources. Currently, the provider only manages the creation, deletion, and listing of Momento caches.

<!-- TODO: update link to point to official Momento provider registry entry -->
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
  api_key = var.api_key
}
```

The provider can use an authentication token (API key) from Momento.
It can be provided through the configuration block, or through the `MOMENTO_API_KEY` environment variable.

### Creating a cache

```hcl
resource "momento_cache" "example" {
  name = "example"
}
```

----------------------------------------------------------------------------------------
For more info, visit our website at [https://gomomento.com](https://gomomento.com)!
