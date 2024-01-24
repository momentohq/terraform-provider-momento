package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccCacheResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccCacheResourceConfig("one"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", "one"),
					resource.TestCheckResourceAttr("momento_cache.test", "id", "one"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "momento_cache.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccCacheResourceConfig("two"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", "two"),
					resource.TestCheckResourceAttr("momento_cache.test", "id", "two"),
				),
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
