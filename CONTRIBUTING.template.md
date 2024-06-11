{{ ossHeader }}

# Welcome to terraform-provider-momento contributing guide :wave:

Thank you for taking your time to contribute to our Terraform provider!
This guide will provide you information to start your own development and testing.
Happy coding :dancer:

## Submitting

If you've found a bug or have a suggestion, please [open an issue in our project](https://github.com/momentohq/terraform-provider-momento/issues).

If you want to submit a change, please [submit a pull request to our project](https://github.com/momentohq/terraform-provider-momento/pulls). Use the normal [Github pull request process](https://docs.github.com/en/pull-requests). 

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) >= 1.5
- [Go](https://go.dev/) >= 1.19

## Developing :computer:

To develop and test the Terraform provider locally, 

1. Clone the repository
2. Enter the repository directory
3. Build the provider using the Go `install` command:

    ```shell
    go install .
    ```

    This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

4. Create a `.terraformrc` file in your home directory that contains following configuration:

    ```hcl
    provider_installation {
      dev_overrides {
          "momentohq/momento" = "<path to where Go installs your binaries>"
      }
      direct {}
    }
    ```

    Typically the path will be a place like `~/go/bin` or `/Users/username/go/bin`.
    This override means your Terraform commands will use the provider you built instead of downloading one from the Terraform Registry.

5. To test out the provider, you can create a `main.tf` file with the following contents:

    ```
    terraform {
      required_providers {
        momento = {
          source = "momentohq/momento"
        }
      }
    }

    provider "momento" {}

    resource "momento_cache" "example" {
      name = "example"
    }
    ```

    Remember to provide the optional `auth_token` argument in the `provider` block, or set the `MOMENTO_AUTH_TOKEN` environment variable before running the provider.

    To test locally, skip `terraform init` and just use `terraform apply`.
    Terraform provides this warning otherwise: "Skip terraform init when using provider development overrides. It is not necessary and may error unexpectedly."

## Tests :zap:

TODO

{{ ossFooter }}
