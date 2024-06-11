package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestListCachesDataSource(t *testing.T) {
	cacheName := "terraform-provider-momento-test-" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create at least one cache to test the data source
			{
				Config: testAccCacheResourceConfig(cacheName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_cache.test", "name", cacheName),
					resource.TestCheckResourceAttr("momento_cache.test", "id", cacheName),
				),
			},
			// Read testing
			{
				Config: testAccCachesDataSourceConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.momento_caches.test", "id", "placeholder"),
					// Exact number of caches may vary depending on the number of caches created by other tests
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
