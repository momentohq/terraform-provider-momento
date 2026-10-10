package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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

// CapacityPoolResourceModel describes the resource data model.
type CapacityPoolResourceModel struct {
	Name         types.String                  `tfsdk:"name"`
	Provisioning CapacityPoolProvisioningModel `tfsdk:"provisioning"`
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

type CapacityPoolData struct {
	Name         string                       `json:"name"`
	Provisioning CapacityPoolProvisioningData `json:"provisioning"`
}

func (r *CapacityPoolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CapacityPoolResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	postUrl := fmt.Sprintf("%s/capacity_pool/%s", r.httpEndpoint, plan.Name.ValueString())

	provisioning := map[string]any{}
	if plan.Provisioning.Flex != nil {
		zones := make([]string, 0, len(plan.Provisioning.Flex.Zones))
		for _, az := range plan.Provisioning.Flex.Zones {
			zones = append(zones, az.ValueString())
		}
		flex := map[string]any{
			"capacity": map[string]int32{
				"min_gib": plan.Provisioning.Flex.Capacity.MinGib.ValueInt32(),
				"max_gib": plan.Provisioning.Flex.Capacity.MaxGib.ValueInt32(),
			},
			"replication": map[string]int32{
				"min_replicas_per_shard": plan.Provisioning.Flex.Replication.MinReplicasPerShard.ValueInt32(),
				"max_replicas_per_shard": plan.Provisioning.Flex.Replication.MaxReplicasPerShard.ValueInt32(),
			},
			"zones": zones,
		}
		provisioning[flexApiFieldKey] = flex
	} else if plan.Provisioning.Cluster != nil {
		zones := make([]string, 0, len(plan.Provisioning.Cluster.Zones))
		for _, az := range plan.Provisioning.Cluster.Zones {
			zones = append(zones, az.ValueString())
		}
		provisioning[clusterApiFieldKey] = map[string]any{
			"instance_type":      plan.Provisioning.Cluster.InstanceType.ValueString(),
			"shard_count":        plan.Provisioning.Cluster.ShardCount.ValueInt32(),
			"replicas_per_shard": plan.Provisioning.Cluster.ReplicasPerShard.ValueInt32(),
			"zones":              zones,
		}
	}

	requestMap := map[string]any{
		"provisioning": provisioning,
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

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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

	if foundPool.Provisioning.Flex != nil {
		state.Provisioning.Flex.Capacity.MinGib = types.Int32Value(int32(foundPool.Provisioning.Flex.Capacity.MinGib))
		state.Provisioning.Flex.Capacity.MaxGib = types.Int32Value(int32(foundPool.Provisioning.Flex.Capacity.MaxGib))
		state.Provisioning.Flex.Replication.MinReplicasPerShard = types.Int32Value(int32(foundPool.Provisioning.Flex.Replication.MinReplicasPerShard))
		state.Provisioning.Flex.Replication.MaxReplicasPerShard = types.Int32Value(int32(foundPool.Provisioning.Flex.Replication.MaxReplicasPerShard))
		zones := make([]types.String, 0, len(foundPool.Provisioning.Flex.Zones))
		for _, az := range foundPool.Provisioning.Flex.Zones {
			zones = append(zones, types.StringValue(az))
		}
		state.Provisioning.Flex.Zones = zones
	} else if foundPool.Provisioning.Cluster != nil {
		state.Provisioning.Cluster.InstanceType = types.StringValue(foundPool.Provisioning.Cluster.InstanceType)
		state.Provisioning.Cluster.ShardCount = types.Int32Value(int32(foundPool.Provisioning.Cluster.ShardCount))
		state.Provisioning.Cluster.ReplicasPerShard = types.Int32Value(int32(foundPool.Provisioning.Cluster.ReplicasPerShard))
		zones := make([]types.String, 0, len(foundPool.Provisioning.Cluster.Zones))
		for _, az := range foundPool.Provisioning.Cluster.Zones {
			zones = append(zones, types.StringValue(az))
		}
		state.Provisioning.Cluster.Zones = zones
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

const poolUpdateTip = "Instead, please create a new Pool, recreate your Databases and data, and destroy the old Pool when you're ready."

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

	provisioning := map[string]any{}
	if plan.Provisioning.Flex != nil {
		zones := make([]string, 0, len(plan.Provisioning.Flex.Zones))
		for _, az := range plan.Provisioning.Flex.Zones {
			zones = append(zones, az.ValueString())
		}
		flex := map[string]any{
			"capacity": map[string]int32{
				"min_gib": plan.Provisioning.Flex.Capacity.MinGib.ValueInt32(),
				"max_gib": plan.Provisioning.Flex.Capacity.MaxGib.ValueInt32(),
			},
			"replication": map[string]int32{
				"min_replicas_per_shard": plan.Provisioning.Flex.Replication.MinReplicasPerShard.ValueInt32(),
				"max_replicas_per_shard": plan.Provisioning.Flex.Replication.MaxReplicasPerShard.ValueInt32(),
			},
			"zones": zones,
		}
		provisioning[flexApiFieldKey] = flex
	} else if plan.Provisioning.Cluster != nil {
		zones := make([]string, 0, len(plan.Provisioning.Cluster.Zones))
		for _, az := range plan.Provisioning.Cluster.Zones {
			zones = append(zones, az.ValueString())
		}
		provisioning[clusterApiFieldKey] = map[string]any{
			"instance_type":      plan.Provisioning.Cluster.InstanceType.ValueString(),
			"shard_count":        plan.Provisioning.Cluster.ShardCount.ValueInt32(),
			"replicas_per_shard": plan.Provisioning.Cluster.ReplicasPerShard.ValueInt32(),
			"zones":              zones,
		}
	}

	requestMap := map[string]any{
		"provisioning": provisioning,
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

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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
