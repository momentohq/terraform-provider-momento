package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestCreateLeaderboardResource(t *testing.T) {
	cacheName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	leaderboardName1 := "terraform-provider-momento-test-" + acctest.RandString(8)

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
			//
			{
				Config: testAccLeaderboardResourceConfig(cacheName1, leaderboardName1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_leaderboard.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_leaderboard.test", "name", leaderboardName1),
					resource.TestCheckResourceAttr("momento_leaderboard.test", "cache_name", cacheName1),
					resource.TestCheckResourceAttr("momento_leaderboard.test", "id", leaderboardName1),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccLeaderboardResourceConfig(cache_name string, leaderboard_name string) string {
	return fmt.Sprintf(`
resource "momento_leaderboard" "test" {
	name = %[1]q
	cache_name = %[2]q
}
`, leaderboard_name, cache_name)
}
