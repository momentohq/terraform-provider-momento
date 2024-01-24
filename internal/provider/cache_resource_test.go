package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccCacheResource(t *testing.T) {
	rName1 := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	rName2 := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccCacheResourceConfig(rName1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", rName1),
					resource.TestCheckResourceAttr("momento_cache.test", "id", rName1),
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
				Config: testAccCacheResourceConfig(rName2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", rName2),
					resource.TestCheckResourceAttr("momento_cache.test", "id", rName2),
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
