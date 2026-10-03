package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestCreateApiKeyResource(t *testing.T) {
	testAccPreCheckV2ApiKey(t)

	keyDescription1 := "terraform provider momento test " + acctest.RandString(8)
	keyDescription2 := "terraform provider momento test " + acctest.RandString(8)
	roleId1 := "r-viewer"
	roleId2 := "r-operator"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Create and Read one API key
			{
				Config: testAccApiKeyConfig(keyDescription1, roleId1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_api_key.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_api_key.test", "description", keyDescription1),
					resource.TestCheckResourceAttr("momento_api_key.test", "role_id", roleId1),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "key_id"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "api_key"),
				),
			},
			// Updating an API key should not be allowed
			{
				Config:      testAccApiKeyConfig(keyDescription2, roleId1),
				ExpectError: regexp.MustCompile("API Key Error"),
			},
			{
				Config:      testAccApiKeyConfig(keyDescription1, roleId2),
				ExpectError: regexp.MustCompile("API Key Error"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestCreateApiKeyResourceWithV1(t *testing.T) {
	testAccPreCheckV1ApiKey(t)

	keyDescription := "terraform-provider-momento-test-" + acctest.RandString(8)
	roleId := "r-viewer"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Creating an API key should require a v2 API key
			{
				Config:      testAccApiKeyConfig(keyDescription, roleId),
				ExpectError: regexp.MustCompile("V2 API Key Required"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccApiKeyConfig(description string, roleId string) string {
	return fmt.Sprintf(`
resource "momento_api_key" "test" {
  description = %[1]q
  role_id = %[2]q
}
`, description, roleId)
}
