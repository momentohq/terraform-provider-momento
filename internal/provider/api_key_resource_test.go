package provider

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestCreateApiKeyResource(t *testing.T) {
	testAccPreCheckV2ApiKey(t)

	keyDescription1 := "terraform provider momento test " + acctest.RandString(8)
	keyDescription2 := "terraform provider momento test " + acctest.RandString(8)
	roleId1 := "r-viewer"
	roleId2 := "r-operator"
	expiry := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Create an API key with a refresh token
			{
				Config: testAccApiKeyConfig(keyDescription1, roleId1, expiry),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_api_key.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_api_key.test", "description", keyDescription1),
					resource.TestCheckResourceAttr("momento_api_key.test", "role_id", roleId1),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "key_id"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "api_key"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "refresh_token"),
				),
			},
			// Re-applying an unchanged config should not replace the key
			{
				Config: testAccApiKeyConfig(keyDescription1, roleId1, expiry),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			// Updating an API key should not be allowed
			{
				Config:      testAccApiKeyConfig(keyDescription2, roleId1, expiry),
				ExpectError: regexp.MustCompile("API Key Cannot Be Updated"),
			},
			{
				Config:      testAccApiKeyConfig(keyDescription1, roleId2, expiry),
				ExpectError: regexp.MustCompile("API Key Cannot Be Updated"),
			},
			{
				Config:      testAccApiKeyConfigWithExcludeRefreshSpecified(keyDescription1, expiry, true),
				ExpectError: regexp.MustCompile("API Key Cannot Be Updated"),
			},
			// Destroying should still work with a changed config
			{
				Config:  testAccApiKeyConfig(keyDescription2, roleId2, expiry),
				Destroy: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_api_key.test", "Destroy"),
					},
				},
			},
		},
	})

	// API key with an expiry but no refresh token:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccApiKeyConfigWithExcludeRefreshSpecified(keyDescription1, expiry, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_api_key.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_api_key.test", "description", keyDescription1),
					resource.TestCheckResourceAttr("momento_api_key.test", "role_id", roleId1),
					resource.TestCheckResourceAttr("momento_api_key.test", "exclude_refresh_token", "true"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "key_id"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "api_key"),
					resource.TestCheckNoResourceAttr("momento_api_key.test", "refresh_token"),
				),
			},
			{
				Config:      testAccApiKeyConfig(keyDescription1, roleId1, expiry),
				ExpectError: regexp.MustCompile("API Key Cannot Be Updated"),
			},
			{
				Config:      testAccApiKeyConfigWithExcludeRefreshSpecified(keyDescription1, expiry, false),
				ExpectError: regexp.MustCompile("API Key Cannot Be Updated"),
			},
		},
	})

	// API key with an expiry and explicitly with a refresh token:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccApiKeyConfigWithExcludeRefreshSpecified(keyDescription1, expiry, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_api_key.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_api_key.test", "description", keyDescription1),
					resource.TestCheckResourceAttr("momento_api_key.test", "role_id", roleId1),
					resource.TestCheckResourceAttr("momento_api_key.test", "exclude_refresh_token", "false"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "key_id"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "api_key"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "refresh_token"),
				),
			},
			{
				Config:      testAccApiKeyConfig(keyDescription1, roleId1, expiry),
				ExpectError: regexp.MustCompile("API Key Cannot Be Updated"),
			},
			{
				Config:      testAccApiKeyConfigWithExcludeRefreshSpecified(keyDescription1, expiry, true),
				ExpectError: regexp.MustCompile("API Key Cannot Be Updated"),
			},
		},
	})

	// API key with no expiry and explicitly with no refresh token:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccApiKeyConfigWithoutExpiry(keyDescription1, roleId1, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("momento_api_key.test", "Create"),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("momento_api_key.test", "description", keyDescription1),
					resource.TestCheckResourceAttr("momento_api_key.test", "role_id", roleId1),
					resource.TestCheckResourceAttr("momento_api_key.test", "exclude_refresh_token", "true"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "key_id"),
					resource.TestCheckResourceAttrSet("momento_api_key.test", "api_key"),
					resource.TestCheckNoResourceAttr("momento_api_key.test", "refresh_token"),
				),
			},
		},
	})

	// API key with an expiry in the past:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccApiKeyConfig(keyDescription1, roleId1, time.Now().Add(-1*time.Hour).Format(time.RFC3339)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Expiry is in the past"),
			},
		},
	})

	// API key with an unparseable expiry:
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccApiKeyConfig(keyDescription1, roleId1, "today i guess"),
				ExpectError: regexp.MustCompile("Invalid RFC"),
			},
		},
	})
}

func TestCreateApiKeyResourceWithV1(t *testing.T) {
	testAccPreCheckV1ApiKey(t)

	keyDescription := "terraform-provider-momento-test-" + acctest.RandString(8)
	roleId := "r-viewer"
	expiry := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Each TestStep represents one `terraform apply`
		Steps: []resource.TestStep{
			// Creating an API key should require a v2 API key
			{
				Config:      testAccApiKeyConfig(keyDescription, roleId, expiry),
				ExpectError: regexp.MustCompile("V2 API Key Required"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccApiKeyConfig(description string, roleId string, expiry string) string {
	return fmt.Sprintf(`
resource "momento_api_key" "test" {
  description = %[1]q
  role_id = %[2]q
  expiry = %[3]q
}
`, description, roleId, expiry)
}

func testAccApiKeyConfigWithExcludeRefreshSpecified(description string, expiry string, excludeRefreshToken bool) string {
	return fmt.Sprintf(`
resource "momento_api_key" "test" {
  description = %[1]q
  role_id = "r-viewer"
  exclude_refresh_token = %[2]t
  expiry = %[3]q
}
`, description, excludeRefreshToken, expiry)
}

func testAccApiKeyConfigWithoutExpiry(description string, roleId string, excludeRefreshToken bool) string {
	return fmt.Sprintf(`
resource "momento_api_key" "test" {
  description = %[1]q
  role_id = %[2]q
  exclude_refresh_token = %[3]t
}
`, description, roleId, excludeRefreshToken)
}
