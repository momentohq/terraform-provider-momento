package provider

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestCreateCapacityPoolResource(t *testing.T) {
	poolName1 := "terraform-provider-momento-test-" + acctest.RandString(8)
	poolName2 := "terraform-provider-momento-test-" + acctest.RandString(8)
	poolName3 := "terraform-provider-momento-test-" + acctest.RandString(8)
	familyName1 := "standard"
	familyName2 := "performance"
	familyCellDefault := "standard"
	zones1 := []string{"usw2-az1"}
	zones2 := []string{"usw2-az3", "usw2-az1"}

	// Flex-mode capacity pool:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			{
				Config:      testAccCapacityPoolConfigFlexIncomplete(poolName1, zones1),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Inappropriate value for attribute "provisioning": attribute "flex".*required`),
			},
			// Create a pool
			{
				Config: testAccCapacityPoolConfigFlex(poolName1, familyName1, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "name", poolName1),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "provisioning.flex.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.capacity.min_gib", "20"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.replication.min_replicas_per_shard", "0"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyName1),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones1))),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.cluster.%"),
				),
			},
			// Renaming a pool should be rejected
			{
				Config:      testAccCapacityPoolConfigFlex(poolName2, familyName1, zones1),
				ExpectError: regexp.MustCompile("Pool Cannot Be Renamed"),
			},
			// Switching a pool's provisioning *mode* should be rejected
			{
				Config:      testAccCapacityPoolConfigCluster(poolName1, zones1),
				ExpectError: regexp.MustCompile("Pool Mode Cannot Be Changed"),
			},
			// Empty zones should be rejected
			{
				Config:      testAccCapacityPoolConfigFlex(poolName1, familyName1, []string{}),
				ExpectError: regexp.MustCompile("zones list must contain at least 1 element"),
			},
			// Adjusting a pool's provisioning *fields* should be allowed
			{
				Config: testAccCapacityPoolConfigFlex(poolName1, familyName2, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "name", poolName1),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "provisioning.flex.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.capacity.min_gib", "20"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.replication.min_replicas_per_shard", "0"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyName2),                // changed
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones2))), // changed
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.cluster.%"),
				),
			},
			// Removing a pool's family configuration should resolve it to the cell default:
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						// An unknown family is what makes Update send "family": "cell_default"
						plancheck.ExpectUnknownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("family")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyCellDefault),
				),
			},
			// Keeping a pool's family configuration removed should keep its family as previously resolved by the API:
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "NoOp"),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("family"), knownvalue.StringExact(familyCellDefault)),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyCellDefault),
				),
			},
			// Destroying should still work with a "renamed" pool
			{
				Config:  testAccCapacityPoolConfigFlex(poolName2, familyName1, zones1),
				Destroy: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Destroy"),
					},
				},
			},
		},
	})

	// Capacity families:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a pool with no specified family
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName2, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Create"),
						plancheck.ExpectUnknownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("family")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyCellDefault),
				),
			},
			// Change family
			{
				Config: testAccCapacityPoolConfigFlex(poolName2, familyName2, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyName2),
				),
			},
			// Back to default
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName2, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						plancheck.ExpectUnknownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("family")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyCellDefault),
				),
			},
		},
	})

	// Cluster-mode capacity pool:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccCapacityPoolConfigClusterIncomplete(poolName1, zones1),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Inappropriate value for attribute "provisioning": attribute "cluster".*required`),
			},
			// Create a pool
			{
				Config: testAccCapacityPoolConfigCluster(poolName3, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "name", poolName3),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "provisioning.cluster.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.instance_type", "t4g.small"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.replicas_per_shard", "0"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.zones.#", strconv.Itoa(len(zones1))),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.%"),
				),
			},
			// Switching a pool's provisioning *mode* should be rejected
			{
				Config:      testAccCapacityPoolConfigFlex(poolName3, familyName1, zones1),
				ExpectError: regexp.MustCompile("Pool Mode Cannot Be Changed"),
			},
			// Empty zones should be rejected
			{
				Config:      testAccCapacityPoolConfigCluster(poolName3, []string{}),
				ExpectError: regexp.MustCompile("zones list must contain at least 1 element"),
			},
			// Adjusting a pool's provisioning *fields* should be allowed
			{
				Config: testAccCapacityPoolConfigCluster(poolName3, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "name", poolName3),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "provisioning.cluster.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.instance_type", "t4g.small"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.replicas_per_shard", "0"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.zones.#", strconv.Itoa(len(zones2))), // changed
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.%"),
				),
			},
		},
	})
}

func testAccCapacityPoolConfigFlex(name string, family string, zones []string) string {
	zonesString := "[]"
	if len(zones) > 0 {
		zonesString = fmt.Sprintf(`["%s"]`, strings.Join(zones, `", "`))
	}
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts { create = "5s" }

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
      family = %[3]q
      zones = %[2]s
    }
  }
}
`, name, zonesString, family)
}

func testAccCapacityPoolConfigFlexWithoutFamily(name string, zones []string) string {
	zonesString := "[]"
	if len(zones) > 0 {
		zonesString = fmt.Sprintf(`["%s"]`, strings.Join(zones, `", "`))
	}
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts { create = "5s" }

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
      zones = %[2]s
    }
  }
}
`, name, zonesString)
}

func testAccCapacityPoolConfigFlexIncomplete(name string, zones []string) string {
	zonesString := "[]"
	if len(zones) > 0 {
		zonesString = fmt.Sprintf(`["%s"]`, strings.Join(zones, `", "`))
	}
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts { create = "5s" }

  name = %[1]q
  provisioning = {
    flex = {
      replication = {
        max_replicas_per_shard = 1
      },
      zones = %[2]s
    }
  }
}
`, name, zonesString)
}

func testAccCapacityPoolConfigCluster(name string, zones []string) string {
	zonesString := "[]"
	if len(zones) > 0 {
		zonesString = fmt.Sprintf(`["%s"]`, strings.Join(zones, `", "`))
	}
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts { create = "5s" }

  name = %[1]q
  provisioning = {
    cluster = {
      instance_type      = "t4g.small",
      shard_count        = 1,
      replicas_per_shard = 0,
      zones = %[2]s
    }
  }
}
`, name, zonesString)
}

func testAccCapacityPoolConfigClusterIncomplete(name string, zones []string) string {
	zonesString := "[]"
	if len(zones) > 0 {
		zonesString = fmt.Sprintf(`["%s"]`, strings.Join(zones, `", "`))
	}
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts { create = "5s" }

  name = %[1]q
  provisioning = {
    cluster = {
      instance_type = "t4g.small",
      zones = %[2]s
    }
  }
}
`, name, zonesString)
}
