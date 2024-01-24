package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccCachesDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Read testing
			{
				Config: testAccCachesDataSourceConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.momento_caches.test", "id", "placeholder"),
					resource.TestCheckResourceAttrSet("data.momento_caches.test", "caches.#"),
				),
			},
		},
	})
}

const testAccCachesDataSourceConfig = `
data "momento_caches" "test" {
}
`
