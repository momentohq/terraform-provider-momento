package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestCreateValkeyDatabaseResource(t *testing.T) {
	databaseName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	databaseName2 := "terraform-provider-momento-test-" + acctest.RandString(8)
	poolName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	poolName2 := "terraform-provider-momento-test-" + acctest.RandString(8)
	allPoolNames := []string{poolName1, poolName2}

	iamRole := "arn:aws:iam::123456789012:role/MomentoMetricsRole"
	region := "us-east-1"

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
					resource.TestCheckNoResourceAttr("momento_valkey_database.test", "metrics_config.%"),
				),
			},
			// Renaming a database should be rejected
			{
				Config:      testAccValkeyDatabaseConfig(databaseName2, poolName1, allPoolNames),
				ExpectError: regexp.MustCompile("Database Cannot Be Renamed"),
			},
			// Moving a database should be rejected, even to an existing pool
			{
				Config:      testAccValkeyDatabaseConfig(databaseName1, poolName2, allPoolNames),
				ExpectError: regexp.MustCompile("Database Cannot Be Moved"),
			},
			// Enabling metrics should be allowed
			{
				Config: testAccValkeyDatabaseConfigWithMetrics(databaseName1, poolName1, allPoolNames, iamRole, &region),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_valkey_database.test", "Update"),
						plancheck.ExpectKnownValue("momento_valkey_database.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("iam_role_arn"),
							knownvalue.StringExact(iamRole)),
						plancheck.ExpectKnownValue("momento_valkey_database.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("region"),
							knownvalue.StringExact(region)),
					},
				},
				ExpectError: regexp.MustCompile(fmt.Sprintf(`'metrics_config': Failed to assume the provided IAM role[\s]+'%s'`, iamRole)),
			},
			// Default region
			{
				Config: testAccValkeyDatabaseConfigWithMetrics(databaseName1, poolName1, allPoolNames, iamRole, nil),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_valkey_database.test", "Update"),
						plancheck.ExpectKnownValue("momento_valkey_database.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("iam_role_arn"),
							knownvalue.StringExact(iamRole)),
						plancheck.ExpectKnownValue("momento_valkey_database.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("region"),
							knownvalue.Null()),
					},
				},
				ExpectError: regexp.MustCompile(fmt.Sprintf(`'metrics_config': Failed to assume the provided IAM role[\s]+'%s'`, iamRole)),
			},
			// Disable metrics
			{
				Config: testAccValkeyDatabaseConfigWithMetricsDisabled(databaseName1, poolName1, allPoolNames),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_valkey_database.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("momento_valkey_database.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_valkey_database.test", "metrics_config.disabled", "true"),
					resource.TestCheckNoResourceAttr("momento_valkey_database.test", "metrics_config.cloud_watch.%"),
				),
			},
			// Back to inheriting metrics
			{
				Config: testAccValkeyDatabaseConfig(databaseName1, poolName1, allPoolNames),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_valkey_database.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("momento_valkey_database.test", "metrics_config.%"),
				),
			},
			// Destroying should still work with a renamed database
			{
				Config:  testAccValkeyDatabaseConfig(databaseName2, poolName1, allPoolNames),
				Destroy: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_valkey_database.test", "Destroy"),
					},
				},
			},
		},
	})
}

func resourceName(name string) string {
	return "test_" + strings.ReplaceAll(name, "-", "_")
}

func poolsAsString(poolNames []string) string {
	var pools strings.Builder
	for _, poolName := range poolNames {
		fmt.Fprintf(&pools, `
resource "momento_capacity_pool" "%[2]s" {
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
`, poolName, resourceName(poolName))
	}
	return pools.String()
}

func testAccValkeyDatabaseConfig(databaseName string, poolNameForDatabase string, poolNames []string) string {
	return fmt.Sprintf(`
resource "momento_valkey_database" "test" {
  name = %[1]q
  pool_name = momento_capacity_pool.%[2]s.name
}

%[3]s
`, databaseName, resourceName(poolNameForDatabase), poolsAsString(poolNames))
}

func testAccValkeyDatabaseConfigWithMetrics(
	databaseName string, poolNameForDatabase string, poolNames []string,
	metricsIamRole string, region *string,
) string {
	var regionString string
	if region != nil {
		regionString = fmt.Sprintf(`region = %q`, *region)
	}
	return fmt.Sprintf(`
resource "momento_valkey_database" "test" {
  name = %[1]q
  pool_name = momento_capacity_pool.%[2]s.name
  metrics_config = {
    cloud_watch = {
      iam_role_arn = %[4]q
      %[5]s
    }
  }
}

%[3]s
`, databaseName, resourceName(poolNameForDatabase), poolsAsString(poolNames), metricsIamRole, regionString)
}

func testAccValkeyDatabaseConfigWithMetricsDisabled(databaseName string, poolNameForDatabase string, poolNames []string) string {
	return fmt.Sprintf(`
resource "momento_valkey_database" "test" {
  name = %[1]q
  pool_name = momento_capacity_pool.%[2]s.name
  metrics_config = {
    disabled = true
  }
}

%[3]s
`, databaseName, resourceName(poolNameForDatabase), poolsAsString(poolNames))
}
