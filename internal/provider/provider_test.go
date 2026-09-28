package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories are used to instantiate a provider during
// acceptance testing. The factory function will be invoked for every Terraform
// CLI command executed to create a provider server to which the CLI can
// reattach.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"momento": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	// You can add code here to run prior to any test case execution, for example assertions
	// about the appropriate environment variables being set are common to see in a pre-check
	// function.
}

// testAccPreCheckV2ApiKey skips tests for APIs that are only available with a
// v2 API key (identified by its accompanying MOMENTO_ENDPOINT).
func testAccPreCheckV2ApiKey(t *testing.T) {
	if os.Getenv("MOMENTO_ENDPOINT") == "" {
		t.Skip("MOMENTO_ENDPOINT not set; this test requires a v2 API key")
	}
}

// testAccPreCheckV1ApiKey skips tests that are only intended to run with a
// v1 API key, legacy API key, or disposable token.
func testAccPreCheckV1ApiKey(t *testing.T) {
	if os.Getenv("MOMENTO_ENDPOINT") != "" {
		t.Skip("MOMENTO_ENDPOINT set; this test verifies rejection behavior when a v2 API key is required but absent")
	}
}
