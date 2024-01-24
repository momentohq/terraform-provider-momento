# Momento Terraform Provider

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

## Development

### Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.5
- [Go](https://golang.org/doc/install) >= 1.19

### Building The Provider

1. Clone the repository
2. Enter the repository directory
3. Build the provider using the Go `install` command:

```shell
go install .
```

This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

4. Create a .terraformrc file that contains following configuration:

```hcl
provider_installation {
  dev_overrides {
      "momento" = "<path to where Go installs your binaries>"
  }
  direct {}
}
```

Typically the path will be a place like `~/go/bin`.

5. Now, your terraform commands will use the provider you built.

### Commands

- `make testacc` - Run the acceptance tests
- `go generate` - Generate documentation
