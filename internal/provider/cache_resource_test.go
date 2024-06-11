package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestCreateCacheResource(t *testing.T) {
	cacheName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	cacheName2 := "terraform-provider-momento-test-" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Create and Read one cache
			{
				Config: testAccCacheResourceConfig(cacheName1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_cache.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", cacheName1),
					resource.TestCheckResourceAttr("momento_cache.test", "id", cacheName1),
				),
			},
			// Creating a cache should be idempotent (no new cache should be created on this second call)
			{
				Config: testAccCacheResourceConfig(cacheName1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_cache.test", "NoOp"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", cacheName1),
					resource.TestCheckResourceAttr("momento_cache.test", "id", cacheName1),
				),
			},
			// Updating the config with new cache name should destroy the old cache and create a new one
			{
				Config: testAccCacheResourceConfig(cacheName2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_cache.test", "DestroyBeforeCreate"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", cacheName2),
					resource.TestCheckResourceAttr("momento_cache.test", "id", cacheName2),
				),
			},
			// Test ImportState method (imports existing resources)
			{
				ResourceName:      "momento_cache.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccCacheResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "momento_cache" "test" {
  name = %[1]q
}
`, name)
}
