package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestCreateCustomRoleResource(t *testing.T) {
	testAccPreCheckV2ApiKey(t)

	roleName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	roleName2 := "terraform-provider-momento-test-" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Create and Read one custom role
			{
				Config: testAccCustomRoleConfig(roleName1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_custom_role.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_custom_role.test", "name", roleName1),
					resource.TestCheckResourceAttrSet("momento_custom_role.test", "id"),
				),
			},
			// Creating a custom role should be idempotent (no new role should be created on this second call)
			{
				Config: testAccCustomRoleConfig(roleName1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_custom_role.test", "NoOp"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_custom_role.test", "name", roleName1),
					resource.TestCheckResourceAttrSet("momento_custom_role.test", "id"),
				),
			},
			// Updating the config with a new role name should update the role in place;
			// the custom roles API supports renaming
			{
				Config: testAccCustomRoleConfig(roleName2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_custom_role.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_custom_role.test", "name", roleName2),
					resource.TestCheckResourceAttrSet("momento_custom_role.test", "id"),
				),
			},
			// Test ImportState method (imports existing resources)
			{
				ResourceName:      "momento_custom_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestCreateCustomRoleResourceWithV1(t *testing.T) {
	testAccPreCheckV1ApiKey(t)

	roleName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	roleName2 := "terraform-provider-momento-test-" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Creating a custom role should require a v2 API key
			{
				Config:      testAccCustomRoleConfig(roleName1),
				ExpectError: regexp.MustCompile("V2 API Key Required"),
			},
			// Updating a custom role should require a v2 API key
			{
				Config:      testAccCustomRoleConfig(roleName2),
				ExpectError: regexp.MustCompile("V2 API Key Required"),
			},
			// Importing a custom role should require a v2 API Key
			{
				ResourceName:  "momento_custom_role.test",
				ImportState:   true,
				ImportStateId: "r-some-role",
				ExpectError:   regexp.MustCompile("V2 API Key Required"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccCustomRoleConfig(name string) string {
	return fmt.Sprintf(`
resource "momento_custom_role" "test" {
  name = %[1]q
  permissions = {
    rules = [{
      type        = "cache"
      permissions = ["read", "write"]
      caches      = { all = true }
      items       = { all = true }
    }]
  }
}
`, name)
}
