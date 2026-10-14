package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestCreateValkeyDatabaseResource(t *testing.T) {
	databaseName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	databaseName2 := "terraform-provider-momento-test-" + acctest.RandString(8)
	poolName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	poolName2 := "terraform-provider-momento-test-" + acctest.RandString(8)
	allPoolNames := []string{poolName1, poolName2}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Create a database
			{
				Config: testAccValkeyDatabaseConfig(databaseName1, poolName1, allPoolNames),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_valkey_database.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_valkey_database.test", "name", databaseName1),
					resource.TestCheckResourceAttr("momento_valkey_database.test", "pool_name", poolName1),
				),
			},
			// Renaming a database should not be allowed
			{
				Config:      testAccValkeyDatabaseConfig(databaseName2, poolName1, allPoolNames),
				ExpectError: regexp.MustCompile("Database Cannot Be Updated"),
			},
			// Moving a database should not be allowed, even to an existing pool
			{
				Config:      testAccValkeyDatabaseConfig(databaseName1, poolName2, allPoolNames),
				ExpectError: regexp.MustCompile("Database Cannot Be Updated"),
			},
			// Destroying should still work with a changed config
			{
				Config:  testAccValkeyDatabaseConfig(databaseName2, poolName1, allPoolNames),
				Destroy: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_valkey_database.test", "Destroy"),
					},
				},
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccValkeyDatabaseConfig(databaseName string, poolNameForDatabase string, poolNames []string) string {
	var pools strings.Builder
	for _, poolName := range poolNames {
		fmt.Fprintf(&pools, `
resource "momento_capacity_pool" "test_%[2]s" {
  timeouts { create = "3m" }

  name = %[1]q
  provisioning = {
    flex = {
      capacity = {
        max_gib = 25,
        min_gib = 20
      },
      replication = {
        max_replicas_per_shard = 1,
        min_replicas_per_shard = 0
      },
      zones = [
	    "usw2-az3",
		"usw2-az1"
	  ]
    }
  }
}
`, poolName, strings.ReplaceAll(poolName, "-", "_"))
	}
	return fmt.Sprintf(`
resource "momento_valkey_database" "test" {
  name = %[1]q
  pool_name = momento_capacity_pool.test_%[2]s.name
}

%[3]s
`, databaseName, strings.ReplaceAll(poolNameForDatabase, "-", "_"), pools.String())
}
