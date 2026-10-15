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
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyName1),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones1))),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.cluster.%"),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "false"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "true"),
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
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyName2),       // changed
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones2))), // changed
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.cluster.%"),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "false"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "true"),
				),
			},
			// Removing a pool's family configuration should resolve it to the cell default:
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						// An unknown resolved_family alongside a null family is what makes Update send "family": "cell_default"
						plancheck.ExpectUnknownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("resolved_family")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.family"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyCellDefault),
				),
			},
			// Keeping a pool's family configuration removed should keep its family as previously resolved by the API:
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "NoOp"),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("resolved_family"), knownvalue.StringExact(familyCellDefault)),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.family"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyCellDefault),
				),
			},
			// Test ImportState method (imports existing resources)
			{
				ResourceName:      "momento_capacity_pool.test",
				ImportState:       true,
				ImportStateVerify: true,
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
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Create"),
						plancheck.ExpectUnknownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("resolved_family")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.family"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyCellDefault),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones1))),
				),
			},
			// Change family
			{
				Config: testAccCapacityPoolConfigFlex(poolName1, familyName2, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyName2),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyName2),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones1))),
				),
			},
			// Back to default
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						plancheck.ExpectUnknownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("resolved_family")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.family"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyCellDefault),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones1))),
				),
			},
			// Updating a different field should keep its family as previously resolved by the API:
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("resolved_family"), knownvalue.StringExact(familyCellDefault)),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.family"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyCellDefault),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones2))), // changed
				),
			},
			// Specifically specify what happens to be the default family
			{
				Config: testAccCapacityPoolConfigFlex(poolName1, familyCellDefault, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("resolved_family"), knownvalue.StringExact(familyCellDefault)),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.family", familyCellDefault),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyCellDefault),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones2))),
				),
			},
			// Remove family again to resolve the potentially-new default family
			{
				Config: testAccCapacityPoolConfigFlexWithoutFamily(poolName1, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						plancheck.ExpectUnknownValue("momento_capacity_pool.test",
							tfjsonpath.New("provisioning").AtMapKey("flex").AtMapKey("resolved_family")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.family"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.resolved_family", familyCellDefault),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.flex.zones.#", strconv.Itoa(len(zones2))),
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
				Config: testAccCapacityPoolConfigCluster(poolName1, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "name", poolName1),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "provisioning.cluster.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.instance_type", "t4g.small"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.replicas_per_shard", "0"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.zones.#", strconv.Itoa(len(zones1))),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.%"),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "false"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "true"),
				),
			},
			// Switching a pool's provisioning *mode* should be rejected
			{
				Config:      testAccCapacityPoolConfigFlex(poolName1, familyName1, zones1),
				ExpectError: regexp.MustCompile("Pool Mode Cannot Be Changed"),
			},
			// Empty zones should be rejected
			{
				Config:      testAccCapacityPoolConfigCluster(poolName1, []string{}),
				ExpectError: regexp.MustCompile("zones list must contain at least 1 element"),
			},
			// Adjusting a pool's provisioning *fields* should be allowed
			{
				Config: testAccCapacityPoolConfigCluster(poolName1, zones2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "name", poolName1),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "provisioning.cluster.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.instance_type", "t4g.small"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.replicas_per_shard", "0"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "provisioning.cluster.zones.#", strconv.Itoa(len(zones2))), // changed
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "provisioning.flex.%"),
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "false"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "true"),
				),
			},
		},
	})

	iamRole1 := "arn:aws:iam::123456789012:role/MomentoMetricsRole-1"
	iamRole2 := "arn:aws:iam::123456789012:role/MomentoMetricsRole-2"
	region := "us-east-1"

	// Metrics config:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a pool with metrics
			{
				Config: testAccCapacityPoolConfigWithMetrics(poolName1, zones1, iamRole1, &region),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Create"),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("iam_role_arn"),
							knownvalue.StringExact(iamRole1)),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("region"),
							knownvalue.StringExact(region)),
					},
				},
				// The API will reject this nonexistent IAM role, but we can check it was sent+received:
				ExpectError: regexp.MustCompile(fmt.Sprintf(`'metrics_config': Failed to assume the provided IAM role[\s]+'%s'`, iamRole1)),
			},
			// Create a pool with metrics disabled
			{
				Config: testAccCapacityPoolConfigWithMetricsBool(poolName1, zones1, "disabled"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "true"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "false"),
				),
			},
			// Enable metrics
			{
				Config: testAccCapacityPoolConfigWithMetrics(poolName1, zones1, iamRole2, &region),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("iam_role_arn"),
							knownvalue.StringExact(iamRole2)),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("region"),
							knownvalue.StringExact(region)),
					},
				},
				ExpectError: regexp.MustCompile(fmt.Sprintf(`'metrics_config': Failed to assume the provided IAM role[\s]+'%s'`, iamRole2)),
			},
			// Default region
			{
				Config: testAccCapacityPoolConfigWithMetrics(poolName1, zones1, iamRole2, nil),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("iam_role_arn"),
							knownvalue.StringExact(iamRole2)),
						plancheck.ExpectKnownValue("momento_capacity_pool.test",
							tfjsonpath.New("metrics_config").AtMapKey("cloud_watch").AtMapKey("region"),
							knownvalue.Null()),
					},
				},
				ExpectError: regexp.MustCompile(fmt.Sprintf(`'metrics_config': Failed to assume the provided IAM role[\s]+'%s'`, iamRole2)),
			},
			// Inherit metrics
			{
				Config: testAccCapacityPoolConfigCluster(poolName1, zones1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "false"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "true"),
				),
			},
			// Specifically specify inheriting metrics
			{
				Config: testAccCapacityPoolConfigWithMetricsBool(poolName1, zones1, "inherit"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "NoOp"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "false"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "true"),
				),
			},
			// Disable metrics
			{
				Config: testAccCapacityPoolConfigWithMetricsBool(poolName1, zones1, "disabled"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_capacity_pool.test", "Update"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("momento_capacity_pool.test", "metrics_config.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.disabled", "true"),
					resource.TestCheckNoResourceAttr("momento_capacity_pool.test", "metrics_config.cloud_watch.%"),
					resource.TestCheckResourceAttr("momento_capacity_pool.test", "metrics_config.inherit", "false"),
				),
			},
		},
	})
}

func zonesAsString(zones []string) string {
	zonesString := "[]"
	if len(zones) > 0 {
		zonesString = fmt.Sprintf(`["%s"]`, strings.Join(zones, `", "`))
	}
	return zonesString
}

func testAccCapacityPoolConfigFlex(name string, family string, zones []string) string {
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts {
    create = "5s"
    delete = "3m" // longer bc we actually want to make sure it's deleted, so tests can reuse pool name
  }

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
`, name, zonesAsString(zones), family)
}

func testAccCapacityPoolConfigFlexWithoutFamily(name string, zones []string) string {
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts {
    create = "5s"
    delete = "3m" // longer bc we actually want to make sure it's deleted, so tests can reuse pool name
  }

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
`, name, zonesAsString(zones))
}

func testAccCapacityPoolConfigFlexIncomplete(name string, zones []string) string {
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts {
    create = "5s"
    delete = "3m" // longer bc we actually want to make sure it's deleted, so tests can reuse pool name
  }

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
`, name, zonesAsString(zones))
}

func testAccCapacityPoolConfigCluster(name string, zones []string) string {
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts {
    create = "5s"
    delete = "3m" // longer bc we actually want to make sure it's deleted, so tests can reuse pool name
  }

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
`, name, zonesAsString(zones))
}

func testAccCapacityPoolConfigClusterIncomplete(name string, zones []string) string {
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts {
    create = "5s"
    delete = "3m" // longer bc we actually want to make sure it's deleted, so tests can reuse pool name
  }

  name = %[1]q
  provisioning = {
    cluster = {
      instance_type = "t4g.small",
      zones = %[2]s
    }
  }
}
`, name, zonesAsString(zones))
}

func testAccCapacityPoolConfigWithMetrics(name string, zones []string, metricsIamRole string, region *string) string {
	var regionString string
	if region != nil {
		regionString = fmt.Sprintf(`region = %q`, *region)
	}
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts {
    create = "5s"
    delete = "3m" // longer bc we actually want to make sure it's deleted, so tests can reuse pool name
  }

  name = %[1]q
  provisioning = {
    cluster = {
      instance_type      = "t4g.small",
      shard_count        = 1,
      replicas_per_shard = 0,
      zones = %[2]s
    }
  }
  metrics_config = {
    cloud_watch = {
      iam_role_arn = %[3]q
      %[4]s
    }
  }
}
`, name, zonesAsString(zones), metricsIamRole, regionString)
}

func testAccCapacityPoolConfigWithMetricsBool(name string, zones []string, boolFieldName string) string {
	return fmt.Sprintf(`
resource "momento_capacity_pool" "test" {
  timeouts {
    create = "5s"
    delete = "3m" // longer bc we actually want to make sure it's deleted, so tests can reuse pool name
  }

  name = %[1]q
  provisioning = {
    cluster = {
      instance_type      = "t4g.small",
      shard_count        = 1,
      replicas_per_shard = 0,
      zones = %[2]s
    }
  }
  metrics_config = {
    %[3]s = true
  }
}
`, name, zonesAsString(zones), boolFieldName)
}
