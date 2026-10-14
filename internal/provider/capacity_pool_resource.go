package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource               = &CapacityPoolResource{}
	_ resource.ResourceWithConfigure  = &CapacityPoolResource{}
	_ resource.ResourceWithModifyPlan = &CapacityPoolResource{}
)

const flexApiFieldKey = "managed"
const clusterApiFieldKey = "explicit"

func NewCapacityPoolResource() resource.Resource {
	return &CapacityPoolResource{}
}

// CapacityPoolResource defines the resource implementation.
type CapacityPoolResource struct {
	httpClient    *http.Client
	httpEndpoint  string
	httpAuthToken string
}

type CapacityBoundsModel struct {
	MinGib types.Int32 `tfsdk:"min_gib"`
	MaxGib types.Int32 `tfsdk:"max_gib"`
}

type ReplicationBoundsModel struct {
	MinReplicasPerShard types.Int32 `tfsdk:"min_replicas_per_shard"`
	MaxReplicasPerShard types.Int32 `tfsdk:"max_replicas_per_shard"`
}

type FlexProvisioningModel struct {
	Capacity    CapacityBoundsModel    `tfsdk:"capacity"`
	Replication ReplicationBoundsModel `tfsdk:"replication"`
	Family      types.String           `tfsdk:"family"`
	Zones       []types.String         `tfsdk:"zones"`
}

type ClusterProvisioningModel struct {
	InstanceType     types.String   `tfsdk:"instance_type"`
	ShardCount       types.Int32    `tfsdk:"shard_count"`
	ReplicasPerShard types.Int32    `tfsdk:"replicas_per_shard"`
	Zones            []types.String `tfsdk:"zones"`
}

type CapacityPoolProvisioningModel struct {
	Flex    *FlexProvisioningModel    `tfsdk:"flex"`
	Cluster *ClusterProvisioningModel `tfsdk:"cluster"`
}

type CloudWatchMetricsConfigModel struct {
	CustomerIamRole types.String `tfsdk:"iam_role_arn"` // "iam_role_arn" to match object store Terraform
	Region          types.String `tfsdk:"region"`
}

type CustomerMetricsConfigModel struct {
	Disabled   types.Bool                    `tfsdk:"disabled"`
	CloudWatch *CloudWatchMetricsConfigModel `tfsdk:"cloud_watch"`
}

// CapacityPoolResourceModel describes the resource data model.
type CapacityPoolResourceModel struct {
	Name          types.String                  `tfsdk:"name"`
	Provisioning  CapacityPoolProvisioningModel `tfsdk:"provisioning"`
	Timeouts      timeouts.Value                `tfsdk:"timeouts"`
	MetricsConfig *CustomerMetricsConfigModel   `tfsdk:"metrics_config"`
}

func (r *CapacityPoolResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_capacity_pool"
}

func (r *CapacityPoolResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Capacity Pool.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the Capacity Pool.",
				Required:            true,
			},
			"provisioning": schema.SingleNestedAttribute{
				MarkdownDescription: "The provisioning configuration for the Pool's capacity. The mode is fixed when the Pool is created.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"flex": schema.SingleNestedAttribute{
						Description: "A provisioning mode where you specify capacity and replication bounds, and Momento sizes the Pool within them. Flex mode is sometimes called \"managed mode\".",
						Optional:    true,
						Validators: []validator.Object{
							objectvalidator.ExactlyOneOf(path.MatchRelative().AtParent().AtName("cluster")),
						},
						Attributes: map[string]schema.Attribute{
							"capacity": schema.SingleNestedAttribute{
								MarkdownDescription: "The Pool's available capacity bounds, in GiB.",
								Required:            true,
								Attributes: map[string]schema.Attribute{
									"min_gib": schema.Int32Attribute{
										MarkdownDescription: "The minimum requested available capacity, in GiB. Set equal to the maximum to pin capacity.",
										Required:            true,
									},
									"max_gib": schema.Int32Attribute{
										MarkdownDescription: "The maximum requested available capacity, in GiB.",
										Required:            true,
									},
								},
							},
							"replication": schema.SingleNestedAttribute{
								MarkdownDescription: "The Pool's replication bounds.",
								Required:            true,
								Attributes: map[string]schema.Attribute{
									"min_replicas_per_shard": schema.Int32Attribute{
										MarkdownDescription: "The minimum replicas per shard. Set equal to the maximum to pin replication.",
										Required:            true,
									},
									"max_replicas_per_shard": schema.Int32Attribute{
										MarkdownDescription: "The maximum replicas per shard.",
										Required:            true,
									},
								},
							},
							"family": schema.StringAttribute{
								MarkdownDescription: "The capacity family to use. When omitted, the Pool resolves to the cell's current default and pins it.",
								Optional:            true,
								Computed:            true,
								PlanModifiers: []planmodifier.String{
									stringplanmodifier.UseStateForUnknown(),
								},
							},
							"zones": schema.ListAttribute{
								MarkdownDescription: "The availability zone IDs across which the Pool's nodes are placed. Must contain at least one zone.",
								Required:            true,
								ElementType:         types.StringType,
								Validators: []validator.List{
									listvalidator.SizeAtLeast(1),
								},
							},
						},
					},
					"cluster": schema.SingleNestedAttribute{
						Description: "A provisioning mode where you specify the instance type, shard count, replicas per shard, and AZ placement directly. Cluster mode is sometimes called \"explicit mode\".",
						Optional:    true,
						Attributes: map[string]schema.Attribute{
							"instance_type": schema.StringAttribute{
								MarkdownDescription: "The instance type for the Pool's nodes (for example, r7g.xlarge).",
								Required:            true,
							},
							"shard_count": schema.Int32Attribute{
								MarkdownDescription: "The number of shards in the Pool.",
								Required:            true,
							},
							"replicas_per_shard": schema.Int32Attribute{
								MarkdownDescription: "The number of replicas per shard.",
								Required:            true,
							},
							"zones": schema.ListAttribute{
								MarkdownDescription: "The availability zone IDs across which the Pool's nodes are placed. Must contain at least one zone.",
								Required:            true,
								ElementType:         types.StringType,
								Validators: []validator.List{
									listvalidator.SizeAtLeast(1),
								},
							},
						},
					},
				},
			},
			"metrics_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Optional configuration for exporting CloudWatch metrics. When omitted, the Pool inherits your account-wide metrics config.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"disabled": schema.BoolAttribute{
						Description: "Disable delivery of this Pool's metrics to your CloudWatch account.",
						Optional:    true,
						Computed:    true,
						Validators: []validator.Bool{
							boolvalidator.Equals(true),
							boolvalidator.ExactlyOneOf(
								path.MatchRelative().AtParent().AtName("cloud_watch"),
							),
						},
					},
					"cloud_watch": schema.SingleNestedAttribute{
						Description: "Deliver this Pool's metrics to your CloudWatch account.",
						Optional:    true,
						Attributes: map[string]schema.Attribute{
							"iam_role_arn": schema.StringAttribute{
								MarkdownDescription: "The ARN of the IAM role that Momento will assume to export metrics.",
								Required:            true,
							},
							"region": schema.StringAttribute{
								MarkdownDescription: "The AWS region where the metrics will be exported to. When omitted, defaults to the region this Pool runs in.",
								Optional:            true,
							},
						},
					},
				},
			},
		},

		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true,
			}),
		},
	}
}

func (r *CapacityPoolResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	clients, ok := req.ProviderData.(MomentoClients)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected MomentoClients, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.httpClient = clients.httpClient
	r.httpEndpoint = clients.httpEndpoint
	r.httpAuthToken = clients.httpAuthToken
}

type CapacityBoundsData struct {
	MinGib uint32 `json:"min_gib"`
	MaxGib uint32 `json:"max_gib"`
}

type ReplicationBoundsData struct {
	MinReplicasPerShard uint32 `json:"min_replicas_per_shard"`
	MaxReplicasPerShard uint32 `json:"max_replicas_per_shard"`
}

type FlexProvisioningData struct {
	Capacity    CapacityBoundsData    `json:"capacity"`
	Replication ReplicationBoundsData `json:"replication"`
	Family      *string               `json:"family"`
	Zones       []string              `json:"zones"`
}

type ClusterProvisioningData struct {
	InstanceType     string   `json:"instance_type"`
	ShardCount       uint32   `json:"shard_count"`
	ReplicasPerShard uint32   `json:"replicas_per_shard"`
	Zones            []string `json:"zones"`
}

type CapacityPoolProvisioningData struct {
	Flex    *FlexProvisioningData    `json:"managed"`  // Must match flexApiFieldKey
	Cluster *ClusterProvisioningData `json:"explicit"` // Must match clusterApiFieldKey
}

type CloudWatchMetricsConfigData struct {
	CustomerIamRole string  `json:"customer_iam_role"`
	Region          *string `json:"region"`
}

type CustomerMetricsConfigData struct {
	Disabled   bool                         `json:"disabled"`
	CloudWatch *CloudWatchMetricsConfigData `json:"cloud_watch"`
	Inherit    bool                         `json:"inherit"`
}

// UnmarshalJSON accepts the API's metrics config, which may be a string OR an object.
func (c *CustomerMetricsConfigData) UnmarshalJSON(b []byte) error {
	*c = CustomerMetricsConfigData{}
	if string(b) == "null" {
		return nil
	}

	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t := true
		switch s {
		case "disabled":
			c.Disabled = t
		case "inherit":
			c.Inherit = t
		default:
			return fmt.Errorf("unrecognized metrics_config value %q", s)
		}
		return nil
	}

	// Decode into a separate struct, since decoding into CustomerMetricsConfigData would recurse.
	var obj struct {
		CloudWatch *CloudWatchMetricsConfigData `json:"cloud_watch"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("unable to unmarshal metrics_config: %w", err)
	}
	c.CloudWatch = obj.CloudWatch
	return nil
}

type CapacityPoolData struct {
	Name          string                       `json:"name"`
	Provisioning  CapacityPoolProvisioningData `json:"provisioning"`
	MetricsConfig *CustomerMetricsConfigData   `json:"metrics_config"`
	Status        string                       `json:"status"`
}

func buildProvisioningRequest(provisioning CapacityPoolProvisioningModel, family any) any {
	var rawZones []types.String
	if provisioning.Flex != nil {
		rawZones = provisioning.Flex.Zones
	} else if provisioning.Cluster != nil {
		rawZones = provisioning.Cluster.Zones
	}
	zones := make([]string, 0, len(rawZones))
	for _, az := range rawZones {
		zones = append(zones, az.ValueString())
	}

	if provisioning.Flex != nil {
		flex := map[string]any{
			"capacity": map[string]int32{
				"min_gib": provisioning.Flex.Capacity.MinGib.ValueInt32(),
				"max_gib": provisioning.Flex.Capacity.MaxGib.ValueInt32(),
			},
			"replication": map[string]int32{
				"min_replicas_per_shard": provisioning.Flex.Replication.MinReplicasPerShard.ValueInt32(),
				"max_replicas_per_shard": provisioning.Flex.Replication.MaxReplicasPerShard.ValueInt32(),
			},
			"zones": zones,
		}
		if family != nil {
			flex["family"] = family
		}
		return map[string]any{
			flexApiFieldKey: flex,
		}
	} else if provisioning.Cluster != nil {
		return map[string]any{
			clusterApiFieldKey: map[string]any{
				"instance_type":      provisioning.Cluster.InstanceType.ValueString(),
				"shard_count":        provisioning.Cluster.ShardCount.ValueInt32(),
				"replicas_per_shard": provisioning.Cluster.ReplicasPerShard.ValueInt32(),
				"zones":              zones,
			},
		}
	}
	return nil
}

func buildMetricsConfigRequest(metricsConfig *CustomerMetricsConfigModel) any {
	if metricsConfig == nil {
		return "inherit"
	} else if metricsConfig.CloudWatch != nil {
		cloud_watch := map[string]string{
			"customer_iam_role": metricsConfig.CloudWatch.CustomerIamRole.ValueString(),
		}
		if !metricsConfig.CloudWatch.Region.IsNull() {
			cloud_watch["region"] = metricsConfig.CloudWatch.Region.ValueString()
		}
		return map[string]any{
			"cloud_watch": cloud_watch,
		}
	} else if metricsConfig.Disabled.ValueBool() {
		return "disabled"
	}
	return nil
}

func readMetricsConfigResponse(metricsConfigData *CustomerMetricsConfigData) *CustomerMetricsConfigModel {
	metricsConfig := &CustomerMetricsConfigModel{
		Disabled: types.BoolValue(metricsConfigData.Disabled),
	}
	if metricsConfigData.CloudWatch != nil {
		metricsConfig.CloudWatch = &CloudWatchMetricsConfigModel{
			CustomerIamRole: types.StringValue(metricsConfigData.CloudWatch.CustomerIamRole),
			Region:          types.StringPointerValue(metricsConfigData.CloudWatch.Region),
		}
	}
	return metricsConfig
}

func (r *CapacityPoolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CapacityPoolResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	client := *r.httpClient
	postUrl := fmt.Sprintf("%s/capacity_pool/%s", r.httpEndpoint, plan.Name.ValueString())

	var family any
	if plan.Provisioning.Flex != nil && !plan.Provisioning.Flex.Family.IsUnknown() {
		family = plan.Provisioning.Flex.Family.ValueString()
	}
	requestMap := map[string]any{
		"provisioning":   buildProvisioningRequest(plan.Provisioning, family),
		"metrics_config": buildMetricsConfigRequest(plan.MetricsConfig),
	}

	// Send request
	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal request body, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)
	postRequest, err := http.NewRequest("POST", postUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to create Capacity Pool, got error: %s", err))
		return
	}
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(postRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create Capacity Pool, got error: %s", err))
		return
	}
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode >= 300 {
		msg := fmt.Sprintf("Unable to create Capacity Pool, got non-200 response: %s %s", httpResp.Status, string(body))
		resp.Diagnostics.AddError("Client Error", msg)
		return
	}

	// Map response body to schema and populate computed attribute values
	var pool CapacityPoolData
	err = json.Unmarshal(body, &pool)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to unmarshal JSON response from creating Capacity Pool, got error: %v", err))
		return
	}

	if pool.Provisioning.Flex != nil {
		// API always resolves and returns capacity family:
		plan.Provisioning.Flex.Family = types.StringPointerValue(pool.Provisioning.Flex.Family)
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	familyConfigured, diags := familyIsConfigured(ctx, req.Config)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateKeyFamilyConfigured, boolAsBytes(familyConfigured))...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Poll until this pool is fully created
	// (may need it for a database)
	r.pollUntilPoolReady(ctx, plan.Name.ValueString(), resp)
}

func (r *CapacityPoolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CapacityPoolResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	deleteUrl := fmt.Sprintf("%s/capacity_pool/%s", r.httpEndpoint, state.Name.ValueString())
	deleteRequest, err := http.NewRequest("DELETE", deleteUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to delete Capacity Pool, got error: %s", err))
		return
	}
	deleteRequest.Header.Set("Authorization", r.httpAuthToken)

	httpResp, err := client.Do(deleteRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete Capacity Pool, got error: %s", err))
		return
	}
	if httpResp.StatusCode == 404 {
		// Already deleted
		return
	}
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete Capacity Pool, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}
}

func (r *CapacityPoolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CapacityPoolResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Find capacity pool
	client := *r.httpClient
	foundPool, err := describeCapacityPool(ctx, client, state.Name.ValueString(), r.httpEndpoint, r.httpAuthToken)
	if foundPool == nil && err == nil {
		// Capacity pool not found, remove from state
		resp.Diagnostics.AddWarning("Capacity Pool Not Found", fmt.Sprintf("The Capacity Pool \"%s\" was not found. It may have been deleted outside of Terraform. Removing from state.", state.Name.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read Capacity Pool, got error: %s", err))
		return
	}
	if foundPool == nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read CapacityPool, name \"%s\" not found", state.Name.ValueString()))
		return
	}

	state.Name = types.StringValue(foundPool.Name)

	var rawZones []string
	if foundPool.Provisioning.Flex != nil {
		rawZones = foundPool.Provisioning.Flex.Zones
	} else if foundPool.Provisioning.Cluster != nil {
		rawZones = foundPool.Provisioning.Cluster.Zones
	}
	zones := make([]types.String, 0, len(rawZones))
	for _, az := range rawZones {
		zones = append(zones, types.StringValue(az))
	}

	if foundPool.Provisioning.Flex != nil {
		state.Provisioning.Flex.Capacity.MinGib = types.Int32Value(int32(foundPool.Provisioning.Flex.Capacity.MinGib))
		state.Provisioning.Flex.Capacity.MaxGib = types.Int32Value(int32(foundPool.Provisioning.Flex.Capacity.MaxGib))
		state.Provisioning.Flex.Replication.MinReplicasPerShard = types.Int32Value(int32(foundPool.Provisioning.Flex.Replication.MinReplicasPerShard))
		state.Provisioning.Flex.Replication.MaxReplicasPerShard = types.Int32Value(int32(foundPool.Provisioning.Flex.Replication.MaxReplicasPerShard))
		state.Provisioning.Flex.Family = types.StringPointerValue(foundPool.Provisioning.Flex.Family)
		state.Provisioning.Flex.Zones = zones
	} else if foundPool.Provisioning.Cluster != nil {
		state.Provisioning.Cluster.InstanceType = types.StringValue(foundPool.Provisioning.Cluster.InstanceType)
		state.Provisioning.Cluster.ShardCount = types.Int32Value(int32(foundPool.Provisioning.Cluster.ShardCount))
		state.Provisioning.Cluster.ReplicasPerShard = types.Int32Value(int32(foundPool.Provisioning.Cluster.ReplicasPerShard))
		state.Provisioning.Cluster.Zones = zones
	}

	if foundPool.MetricsConfig != nil && !foundPool.MetricsConfig.Inherit {
		state.MetricsConfig = readMetricsConfigResponse(foundPool.MetricsConfig)
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

const poolUpdateTip = "Instead, please create a new Pool, recreate your Databases and data, and destroy the old Pool when you're ready."

// Private state key recording whether `family` was set in config at the *previous* apply,
// so we know if a null `family` is new (request current default) or existing (keep pinned default).
const privateKeyFamilyConfigured = "family_configured"

var capacityFamilyPath = path.Root("provisioning").AtName("flex").AtName("family")

func familyIsConfigured(ctx context.Context, config tfsdk.Config) (bool, diag.Diagnostics) {
	var familyConfigured types.String
	diags := config.GetAttribute(ctx, capacityFamilyPath, &familyConfigured)
	return !familyConfigured.IsNull(), diags
}

func boolAsBytes(isSet bool) []byte {
	if isSet {
		return []byte("true")
	}
	return []byte("false")
}

func bytesAsBool(isSet []byte) bool {
	return string(isSet) == "true"
}

func (r *CapacityPoolResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Only run during updates: skip Create (state null), Delete (plan null), or unconfigured provider.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || r.httpClient == nil {
		return
	}

	var plan, state CapacityPoolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Name.Equal(state.Name) {
		resp.Diagnostics.AddWarning(
			"Capacity Pool Cannot Be Renamed",
			fmt.Sprintf(
				"Capacity Pool resource does not support renaming, so applying this plan will fail for Pool %s. %s",
				state.Name.ValueString(), poolUpdateTip))
	}

	if (plan.Provisioning.Flex != nil && state.Provisioning.Flex == nil) || (plan.Provisioning.Cluster != nil && state.Provisioning.Cluster == nil) {
		resp.Diagnostics.AddWarning(
			"Capacity Pool Mode Cannot Be Changed",
			fmt.Sprintf(
				"Capacity Pool resource does not support changing its provisioning mode, so applying this plan will fail for Pool %s. %s",
				state.Name.ValueString(), poolUpdateTip))
	}

	if plan.Provisioning.Flex != nil && state.Provisioning.Flex != nil {
		// If config omits Optional+Computed attribute like `family`,
		// Terraform copies state->plan, so check config directly instead:
		familyIsConfigured, diags := familyIsConfigured(ctx, req.Config)
		resp.Diagnostics.Append(diags...)
		familyWasConfiguredBytes, diags := req.Private.GetKey(ctx, privateKeyFamilyConfigured)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		familyWasConfigured := bytesAsBool(familyWasConfiguredBytes)

		// When `family` is newly removed from config, API will resolve and pin the current cell default on apply.
		if familyWasConfigured && !familyIsConfigured && !state.Provisioning.Flex.Family.IsNull() {
			plan.Provisioning.Flex.Family = types.StringUnknown()
		}
	}

	if plan.MetricsConfig != nil && plan.MetricsConfig.CloudWatch != nil {
		plan.MetricsConfig.Disabled = types.BoolValue(false)
	}

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *CapacityPoolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CapacityPoolResourceModel

	// Retrieve values from the plan and current state
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Name.Equal(state.Name) {
		resp.Diagnostics.AddError(
			"Capacity Pool Cannot Be Renamed",
			fmt.Sprintf("Capacity Pool resource does not support renaming. %s", poolUpdateTip))
		return
	}
	if (plan.Provisioning.Flex != nil && state.Provisioning.Flex == nil) || (plan.Provisioning.Cluster != nil && state.Provisioning.Cluster == nil) {
		resp.Diagnostics.AddError(
			"Capacity Pool Mode Cannot Be Changed",
			fmt.Sprintf("Capacity Pool resource does not support changing its provisioning mode. %s", poolUpdateTip))
		return
	}

	client := *r.httpClient
	patchUrl := fmt.Sprintf("%s/capacity_pool/%s", r.httpEndpoint, plan.Name.ValueString())

	// The capacity pool API treats `family` differently during create vs update,
	// but the Terraform provider accepts the same config for both and must translate updates for the API:
	// - create: nil -> null for cell default. Otherwise, pass along "specified-family-name"
	// - update: nil -> "cell_default". Otherwise, "specified-family-name" (even "cell_default") -> {"name": "specified-family-name"}
	var family any
	if plan.Provisioning.Flex != nil {
		if plan.Provisioning.Flex.Family.IsUnknown() {
			family = "cell_default"
		} else {
			family = map[string]string{
				"name": plan.Provisioning.Flex.Family.ValueString(),
			}
		}
	}
	requestMap := map[string]any{
		"provisioning":   buildProvisioningRequest(plan.Provisioning, family),
		"metrics_config": buildMetricsConfigRequest(plan.MetricsConfig),
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal request body, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)
	patchRequest, err := http.NewRequest("PATCH", patchUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to update Capacity Pool, got error: %s", err))
		return
	}
	patchRequest.Header.Set("Content-Type", "application/json")
	patchRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(patchRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update Capacity Pool, got error: %s", err))
		return
	}
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update Capacity Pool, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}

	// Map response body to schema and populate computed attribute values
	var pool CapacityPoolData
	err = json.Unmarshal(body, &pool)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to unmarshal JSON response from updating Capacity Pool, got error: %v", err))
		return
	}

	if pool.Provisioning.Flex != nil {
		plan.Provisioning.Flex.Family = types.StringPointerValue(pool.Provisioning.Flex.Family)
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	familyConfigured, diags := familyIsConfigured(ctx, req.Config)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateKeyFamilyConfigured, boolAsBytes(familyConfigured))...)
}

func describeCapacityPool(ctx context.Context, client http.Client, name string, httpEndpoint string, httpAuthToken string) (*CapacityPoolData, error) {
	getRequest, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/capacity_pool/%s", httpEndpoint, name), nil)
	if err != nil {
		return nil, err
	}
	getRequest.Header.Set("Authorization", httpAuthToken)
	getResp, err := client.Do(getRequest)
	if getResp != nil {
		defer func() { _ = getResp.Body.Close() }()
	}
	if err != nil {
		return nil, err
	}
	// Do not error if 404 not found
	if getResp.StatusCode == 404 {
		return nil, nil
	}
	if getResp.StatusCode >= 300 {
		body, _ := io.ReadAll(getResp.Body)
		return nil, fmt.Errorf("unable to describe Capacity Pool, got non-200 response: %s %s", getResp.Status, string(body))
	}

	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var pool CapacityPoolData
	err = json.Unmarshal(bodyBytes, &pool)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return &pool, nil
}

func (r *CapacityPoolResource) pollUntilPoolReady(ctx context.Context, poolName string, resp *resource.CreateResponse) {
	// Poll until pool status is "active", log any errors but do not stop polling
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			resp.Diagnostics.AddWarning(
				"Capacity Pool Creation",
				fmt.Sprintf(
					"Timed out waiting for Capacity Pool \"%[1]s\" to become active: %v. The Pool may still be creating."+
						" If your other resources need this Pool, poll its status with `momento pool get-status -n \"%[1]s\"` (Momento CLI),"+
						" then run `terraform apply` again when it's active.", poolName, ctx.Err()),
			)
			return
		case <-ticker.C:
			foundPool, err := describeCapacityPool(ctx, *r.httpClient, poolName, r.httpEndpoint, r.httpAuthToken)
			if foundPool != nil && foundPool.Status == "active" {
				return
			} else if err == nil && foundPool == nil {
				// pool not found, which could be a transient state during creation before the pool is fully registered, keep polling
			} else if err != nil || foundPool == nil {
				resp.Diagnostics.AddWarning("Describe after create error", fmt.Sprintf("Error: %s. Continuing to poll until Pool status is Active", err))
			}
		}
	}
}
