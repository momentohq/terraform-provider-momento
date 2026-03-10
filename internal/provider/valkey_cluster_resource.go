package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource              = &ValkeyClusterResource{}
	_ resource.ResourceWithConfigure = &ValkeyClusterResource{}
)

func NewValkeyClusterResource() resource.Resource {
	return &ValkeyClusterResource{}
}

// ValkeyClusterResource defines the resource implementation.
type ValkeyClusterResource struct {
	httpClient    *http.Client
	httpEndpoint  string
	httpAuthToken string
}

type ShardPlacementModel struct {
	Index                    types.Int64    `tfsdk:"index"`
	AvailabilityZone         types.String   `tfsdk:"availability_zone"`
	ReplicaAvailabilityZones []types.String `tfsdk:"replica_availability_zones"`
}

// ValkeyClusterResourceModel describes the resource data model.
type ValkeyClusterResourceModel struct {
	Id                  types.String          `tfsdk:"id"`
	ClusterName         types.String          `tfsdk:"cluster_name"`
	NodeInstanceType    types.String          `tfsdk:"node_instance_type"`
	ShardCount          types.Int64           `tfsdk:"shard_count"`
	ReplicationFactor   types.Int64           `tfsdk:"replication_factor"`
	EnforceShardMultiAz types.Bool            `tfsdk:"enforce_shard_multi_az"`
	ShardPlacements     []ShardPlacementModel `tfsdk:"shard_placements"`
	Timeouts            timeouts.Value        `tfsdk:"timeouts"`
}

func (r *ValkeyClusterResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_valkey_cluster"
}

func (r *ValkeyClusterResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Valkey Cluster.",

		Attributes: map[string]schema.Attribute{
			// The testing framework requires an id attribute to be present in every data source and resource
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the Valkey Cluster.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cluster_name": schema.StringAttribute{
				MarkdownDescription: "Name of the Valkey Cluster.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"node_instance_type": schema.StringAttribute{
				MarkdownDescription: "The instance type for nodes in the Valkey Cluster. Please refer to https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/CacheNodes.SupportedTypes.html for supported instance types.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"shard_count": schema.Int64Attribute{
				MarkdownDescription: "The number of shards.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"replication_factor": schema.Int64Attribute{
				MarkdownDescription: "The number of replicas per shard.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"enforce_shard_multi_az": schema.BoolAttribute{
				MarkdownDescription: "Whether to enforce multi-AZ placement for shards.",
				Required:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"shard_placements": schema.ListNestedAttribute{
				MarkdownDescription: "Optional explicit placement configuration for shards. If not specified, placements are determined automatically.",
				Optional:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"index": schema.Int64Attribute{
							MarkdownDescription: "The 0-based index of the shard.",
							Required:            true,
						},
						"availability_zone": schema.StringAttribute{
							MarkdownDescription: "The availability zone for the primary node.",
							Required:            true,
						},
						"replica_availability_zones": schema.ListAttribute{
							ElementType:         types.StringType,
							MarkdownDescription: "The availability zones for replica nodes.",
							Required:            true,
						},
					},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func validateCreateValkeyClusterTerraformPlan(plan *ValkeyClusterResourceModel) *AttributeError {
	if plan.ClusterName.IsNull() || plan.ClusterName.IsUnknown() || plan.ClusterName.ValueString() == "" {
		return &AttributeError{
			AttributePath: path.Root("cluster_name"),
			Summary:       "Missing required value",
			Detail:        "The Valkey Cluster name is required.",
		}
	}
	if plan.NodeInstanceType.IsNull() || plan.NodeInstanceType.IsUnknown() || plan.NodeInstanceType.ValueString() == "" {
		return &AttributeError{
			AttributePath: path.Root("node_instance_type"),
			Summary:       "Missing required value",
			Detail:        "The node instance type is required.",
		}
	}
	if plan.ShardCount.IsNull() || plan.ShardCount.IsUnknown() || plan.ShardCount.ValueInt64() <= 0 {
		return &AttributeError{
			AttributePath: path.Root("shard_count"),
			Summary:       "Invalid value",
			Detail:        "Shard count must be a positive integer.",
		}
	}
	if plan.ReplicationFactor.IsNull() || plan.ReplicationFactor.IsUnknown() || plan.ReplicationFactor.ValueInt64() < 0 {
		return &AttributeError{
			AttributePath: path.Root("replication_factor"),
			Summary:       "Invalid value",
			Detail:        "Replication factor must be a non-negative integer.",
		}
	}
	if plan.EnforceShardMultiAz.IsNull() || plan.EnforceShardMultiAz.IsUnknown() {
		return &AttributeError{
			AttributePath: path.Root("enforce_shard_multi_az"),
			Summary:       "Missing required value",
			Detail:        "The enforce_shard_multi_az boolean value is required.",
		}
	}
	// Validate length of shard_placements matches shard_count, number of replica_availability_zones in each shard placement
	// matches replication_factor, and that shard indexes are non-negative. Return any validation errors in the response diagnostics.
	if plan.ShardPlacements != nil {
		if len(plan.ShardPlacements) != int(plan.ShardCount.ValueInt64()) {
			return &AttributeError{
				AttributePath: path.Root("shard_placements"),
				Summary:       "Invalid shard placements",
				Detail:        fmt.Sprintf("Number of shard placements must match shard count (%d).", plan.ShardCount.ValueInt64()),
			}
		}

		for i, sp := range plan.ShardPlacements {
			if sp.Index.IsNull() || sp.Index.IsUnknown() || sp.Index.ValueInt64() < 0 {
				return &AttributeError{
					AttributePath: path.Root("shard_placements").AtListIndex(i).AtName("index"),
					Summary:       "Invalid value",
					Detail:        "Shard index must be a non-negative integer.",
				}
			}
			if len(sp.ReplicaAvailabilityZones) != int(plan.ReplicationFactor.ValueInt64()) {
				return &AttributeError{
					AttributePath: path.Root("shard_placements").AtListIndex(i).AtName("replica_availability_zones"),
					Summary:       "Invalid value",
					Detail:        fmt.Sprintf("Number of replica availability zones must match replication factor (%d).", plan.ReplicationFactor.ValueInt64()),
				}
			}
		}
	}
	return nil
}

func (r *ValkeyClusterResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ValkeyClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ValkeyClusterResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 120*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	if validationErr := validateCreateValkeyClusterTerraformPlan(&plan); validationErr != nil {
		resp.Diagnostics.AddAttributeError(
			validationErr.AttributePath,
			validationErr.Summary,
			validationErr.Detail,
		)
		return
	}

	client := *r.httpClient
	postUrl := fmt.Sprintf("%s/ec-cluster", r.httpEndpoint)

	// Create map of request body to marshal into JSON
	requestMap := map[string]interface{}{
		"name":                   plan.ClusterName.ValueString(),
		"node_instance_type":     plan.NodeInstanceType.ValueString(),
		"shard_count":            plan.ShardCount.ValueInt64(),
		"replication_factor":     plan.ReplicationFactor.ValueInt64(),
		"enforce_shard_multi_az": plan.EnforceShardMultiAz.ValueBool(),
	}
	if len(plan.ShardPlacements) > 0 {
		placements := make([]map[string]interface{}, len(plan.ShardPlacements))
		for i, sp := range plan.ShardPlacements {
			replicaAZs := make([]string, len(sp.ReplicaAvailabilityZones))
			for j, az := range sp.ReplicaAvailabilityZones {
				replicaAZs[j] = az.ValueString()
			}
			placements[i] = map[string]interface{}{
				"shard_index":                sp.Index.ValueInt64(),
				"availability_zone":          sp.AvailabilityZone.ValueString(),
				"replica_availability_zones": replicaAZs,
			}
		}
		requestMap["shard_placements"] = placements
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal request body, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)
	postRequest, err := http.NewRequest("POST", postUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to create valkey cluster, got error: %s", err))
		return
	}
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(postRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create valkey cluster, got error: %s", err))
		return
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create valkey cluster, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}

	// Map response body to schema and populate computed attribute values
	plan.Id = types.StringValue(plan.ClusterName.ValueString())

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	// Poll until cluster status is "Active"
	r.pollUntilClusterReady(ctx, plan.ClusterName.ValueString(), resp)
}

func (r *ValkeyClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ValkeyClusterResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, diags := state.Timeouts.Delete(ctx, 120*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	if err := r.deleteClusterAndPollUntilGone(ctx, state.ClusterName.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cluster Deletion Failed", fmt.Sprintf("Cluster \"%s\" deletion failed with error: %s. You may need to manually delete the cluster.", state.ClusterName.ValueString(), err.Error()))
		return
	}
}

func (r *ValkeyClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ValkeyClusterResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Find valkey cluster
	client := *r.httpClient
	foundCluster, err := describeValkeyCluster(client, state.ClusterName.ValueString(), r.httpEndpoint, r.httpAuthToken)
	if err == nil && foundCluster == nil {
		resp.Diagnostics.AddWarning("Cluster Not Found", fmt.Sprintf("Cluster with name \"%s\" not found, removing from state", state.ClusterName.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to describe valkey cluster, got error: %s", err))
		return
	}
	if foundCluster == nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read valkey cluster, cluster with name \"%s\" not found", state.ClusterName.ValueString()))
		return
	}

	if len(foundCluster.Errors) > 0 {
		resp.Diagnostics.AddWarning("Valkey Cluster Error", fmt.Sprintf("Found valkey cluster \"%s\" with errors: %v", foundCluster.Name, foundCluster.Errors))
	}

	state.Id = types.StringValue(foundCluster.Name)
	state.ClusterName = types.StringValue(foundCluster.Name)
	state.NodeInstanceType = types.StringValue(foundCluster.NodeInstanceType)
	state.ShardCount = types.Int64Value(foundCluster.ShardCount)
	state.ReplicationFactor = types.Int64Value(foundCluster.ReplicationFactor)
	state.EnforceShardMultiAz = types.BoolValue(foundCluster.EnforceShardMultiAz)

	// reset the list of shard placements before repopulating from the response
	state.ShardPlacements = nil
	for _, sp := range foundCluster.ShardPlacements {
		state.ShardPlacements = append(state.ShardPlacements, ShardPlacementModel{
			Index:            types.Int64Value(sp.ShardIndex),
			AvailabilityZone: types.StringValue(sp.AvailabilityZone),
			ReplicaAvailabilityZones: func() []types.String {
				replicaAZs := make([]types.String, len(sp.ReplicaAvailabilityZones))
				for j, az := range sp.ReplicaAvailabilityZones {
					replicaAZs[j] = types.StringValue(az)
				}
				return replicaAZs
			}(),
		})
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *ValkeyClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read Terraform prior state data into the model
	var currentState ValkeyClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &currentState)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read Terraform planned state into the model
	var plan ValkeyClusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Updating name is not allowed since it might be used by an object store, require explicit delete and recreate
	if plan.ClusterName.ValueString() != currentState.ClusterName.ValueString() {
		resp.Diagnostics.AddError("Invalid Update", "Updating the cluster name is not allowed. Please manually delete and recreate the resource.")
		return
	}

	updateTimeout, diags := plan.Timeouts.Update(ctx, 120*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	if validationErr := validateCreateValkeyClusterTerraformPlan(&plan); validationErr != nil {
		resp.Diagnostics.AddAttributeError(
			validationErr.AttributePath,
			validationErr.Summary,
			validationErr.Detail,
		)
		return
	}

	// Compare requested state with current state and make the appropriate update calls
	diff := determineDiff(currentState, plan)

	// Updates to shard_placements without accompanying change to shard_count or replication_factor are not allowed,
	// indicate the resource must be manually deleted and recreated
	if diff["shard_placements"] && !diff["shard_count"] && !diff["replication_factor"] {
		resp.Diagnostics.AddError(
			"Invalid Update",
			"Updates to shard_placements without accompanying change to shard_count or replication_factor are not allowed. Please manually delete and recreate the resource.",
		)
		return
	}

	// The replica/shard updates may accept shard_placements updates, but not a change in primary AZ for each shard
	if determineIfShardAZChanged(currentState.ShardPlacements, plan.ShardPlacements) {
		resp.Diagnostics.AddError(
			"Invalid Update",
			"Updates to shard_placements that change the primary AZ for a shard are not allowed. Please manually delete and recreate the resource.",
		)
		return
	}

	// If replication_factor is changing from nonzero to 0, enforce_shard_multi_az must be set to false first.
	// Else it'll fail with "Invalid Argument: Must have at least 1 replica for Multi-AZ enabled Replication Group"
	if plan.ReplicationFactor.ValueInt64() == 0 && currentState.ReplicationFactor.ValueInt64() > 0 {
		// Ask user to update enforce_shard_multi_az accordingly
		if plan.EnforceShardMultiAz.ValueBool() {
			resp.Diagnostics.AddError("Invalid Update", "enforce_shard_multi_az must be set to false before setting replication_factor to 0. Please update the resource to set enforce_shard_multi_az to false before retrying")
			return
		}

		var enforceShardMultiAz *bool
		if diff["enforce_shard_multi_az"] {
			valueBool := plan.EnforceShardMultiAz.ValueBool()
			enforceShardMultiAz = &valueBool
			err := r.updateReplicationGroup(currentState.ClusterName.ValueString(), nil, enforceShardMultiAz)
			if err != nil {
				resp.Diagnostics.AddError(
					"Failed to update replication group",
					fmt.Sprintf("Error updating replication group for cluster %s: %s", currentState.ClusterName.ValueString(), err.Error()),
				)
				return
			}
			r.pollUntilClusterUpdated(ctx, plan.ClusterName.ValueString(), resp)
		}
	}

	if diff["replication_factor"] {
		// If not changing number of shards, then send shard placements as is
		updatedCurrentShardPlacements := plan.ShardPlacements

		// Else update replication_factor for existing shards first
		if plan.ShardCount.ValueInt64() != currentState.ShardCount.ValueInt64() {
			// Make copy of current shard placements to modify for the replication factor update, so that we don't mutate the current state shard placements in case we need to use them for a subsequent shard count update if both replication_factor and shard_count are changing
			updatedCurrentShardPlacements = make([]ShardPlacementModel, len(currentState.ShardPlacements))
			for i, sp := range currentState.ShardPlacements {
				replicaAZs := make([]types.String, len(sp.ReplicaAvailabilityZones))
				copy(replicaAZs, sp.ReplicaAvailabilityZones)
				updatedCurrentShardPlacements[i] = ShardPlacementModel{
					Index:                    sp.Index,
					AvailabilityZone:         sp.AvailabilityZone,
					ReplicaAvailabilityZones: replicaAZs,
				}
			}

			// if going to 0, make all the shards have empty replica_availability_zones
			if plan.ReplicationFactor.ValueInt64() == 0 {
				for i := range updatedCurrentShardPlacements {
					updatedCurrentShardPlacements[i].ReplicaAvailabilityZones = []types.String{}
				}
			} else {
				// if decreasing replication_factor, trim the number of replica availability zones for each shard to match the new replication factor
				if plan.ReplicationFactor.ValueInt64() < currentState.ReplicationFactor.ValueInt64() {
					for i := range updatedCurrentShardPlacements {
						updatedCurrentShardPlacements[i].ReplicaAvailabilityZones = updatedCurrentShardPlacements[i].ReplicaAvailabilityZones[:plan.ReplicationFactor.ValueInt64()]
					}
				} else {
					// if increasing replication_factor, keep the existing replica availability zones and add new ones in the same AZ as the primary for the new replicas
					for i := range updatedCurrentShardPlacements {
						currentReplicaAZs := updatedCurrentShardPlacements[i].ReplicaAvailabilityZones
						primaryAZ := updatedCurrentShardPlacements[i].AvailabilityZone
						for j := int64(len(currentReplicaAZs)); j < plan.ReplicationFactor.ValueInt64(); j++ {
							updatedCurrentShardPlacements[i].ReplicaAvailabilityZones = append(updatedCurrentShardPlacements[i].ReplicaAvailabilityZones, primaryAZ)
						}
					}
				}
			}
		}

		// Increase replication factor
		if plan.ReplicationFactor.ValueInt64() > currentState.ReplicationFactor.ValueInt64() {
			err := r.increaseReplicaCount(currentState.ClusterName.ValueString(), int(plan.ReplicationFactor.ValueInt64()), updatedCurrentShardPlacements)
			if err != nil {
				resp.Diagnostics.AddError(
					"Failed to increase replication factor",
					fmt.Sprintf("Error increasing replication factor for cluster %s: %s", currentState.ClusterName.ValueString(), err.Error()),
				)
				return
			}
		}

		// Decrease replication factor
		if plan.ReplicationFactor.ValueInt64() < currentState.ReplicationFactor.ValueInt64() {
			err := r.decreaseReplicaCount(currentState.ClusterName.ValueString(), int(plan.ReplicationFactor.ValueInt64()), updatedCurrentShardPlacements)
			if err != nil {
				resp.Diagnostics.AddError(
					"Failed to decrease replication factor",
					fmt.Sprintf("Error decreasing replication factor for cluster %s: %s", currentState.ClusterName.ValueString(), err.Error()),
				)
				return
			}
		}

		r.pollUntilClusterUpdated(ctx, plan.ClusterName.ValueString(), resp)
	}

	if diff["shard_count"] {
		// Increase shard count
		if plan.ShardCount.ValueInt64() > currentState.ShardCount.ValueInt64() {
			// If increasing shard_count and shard_placements was not specified, then pass only shard_count (placements will be nil anyway)
			err := r.increaseShardCount(currentState.ClusterName.ValueString(), int(plan.ShardCount.ValueInt64()), plan.ShardPlacements)
			if err != nil {
				if strings.Contains(err.Error(), "Availability zones in node group configuration does not match actual availability zones for existing cache clusters") {
					resp.Diagnostics.AddError(
						"Failed to increase shard count due to AZ mismatch",
						fmt.Sprintf("Error increasing shard count for cluster %s: %s. This error can occur when replica or shards end up in AZs that were not specified in the terraform resource. Try calling the describe API on your cluster and updating the terraform resource with the correct AZs, or contact Momento support for assistance.", currentState.ClusterName.ValueString(), err.Error()),
					)
					return
				}
				resp.Diagnostics.AddError(
					"Failed to increase shard count",
					fmt.Sprintf("Error increasing shard count for cluster %s: %s", currentState.ClusterName.ValueString(), err.Error()),
				)
				return
			}
		}

		// Decrease shard count
		if plan.ShardCount.ValueInt64() < currentState.ShardCount.ValueInt64() {
			// If decreasing shard_count and shard_placements weren't specified, then pass the indexes of the shards to remove based on the difference between current and planned shard counts
			if len(plan.ShardPlacements) == 0 {
				resp.Diagnostics.AddError("Specify decrease shard_count shard_placements", "When decreasing shard_count, shard_placements must also be specified to indicate which shards to remove. Please update the plan to include shard_placements with the indexes of the shards to remove.")
				return
			}
			// Calculate which shard indexes to remove based on the difference between current and planned shard placements
			plannedIndexes := make(map[int64]bool, len(plan.ShardPlacements))
			for _, sp := range plan.ShardPlacements {
				plannedIndexes[sp.Index.ValueInt64()] = true
			}
			var shardsToRemove []int
			for _, sp := range currentState.ShardPlacements {
				if !plannedIndexes[sp.Index.ValueInt64()] {
					shardsToRemove = append(shardsToRemove, int(sp.Index.ValueInt64()))
				}
			}
			if err := r.decreaseShardCount(currentState.ClusterName.ValueString(), int(plan.ShardCount.ValueInt64()), shardsToRemove); err != nil {
				resp.Diagnostics.AddError(
					"Failed to decrease shard count",
					fmt.Sprintf("Error decreasing shard count for cluster %s: %s", currentState.ClusterName.ValueString(), err.Error()),
				)
				return
			}
		}

		r.pollUntilClusterUpdated(ctx, plan.ClusterName.ValueString(), resp)
	}

	// Regardless of shard_placements, updateReplicationGroup if node_instance_type and/or enforce_shard_multi_az are updated
	if diff["node_instance_type"] || diff["enforce_shard_multi_az"] {
		var nodeInstanceType *string
		if diff["node_instance_type"] {
			valueString := plan.NodeInstanceType.ValueString()
			nodeInstanceType = &valueString
		}
		var enforceShardMultiAz *bool
		if diff["enforce_shard_multi_az"] {
			valueBool := plan.EnforceShardMultiAz.ValueBool()
			enforceShardMultiAz = &valueBool
		}
		err := r.updateReplicationGroup(currentState.ClusterName.ValueString(), nodeInstanceType, enforceShardMultiAz)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to update replication group",
				fmt.Sprintf("Error updating replication group for cluster %s: %s", currentState.ClusterName.ValueString(), err.Error()),
			)
			return
		}
		r.pollUntilClusterUpdated(ctx, plan.ClusterName.ValueString(), resp)
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func shardPlacementsToAPIFormat(shardPlacements []ShardPlacementModel) []map[string]interface{} {
	placements := make([]map[string]interface{}, len(shardPlacements))
	for i, sp := range shardPlacements {
		replicaAZs := make([]string, len(sp.ReplicaAvailabilityZones))
		for j, az := range sp.ReplicaAvailabilityZones {
			replicaAZs[j] = az.ValueString()
		}
		placements[i] = map[string]interface{}{
			"shard_index":                sp.Index.ValueInt64(),
			"availability_zone":          sp.AvailabilityZone.ValueString(),
			"replica_availability_zones": replicaAZs,
		}
	}
	return placements
}

func determineIfShardAZChanged(currentShards []ShardPlacementModel, plannedShards []ShardPlacementModel) bool {
	// Compare primary availability zones by shard index so that changes are
	// detected even when the number of shards changes or their order differs.
	currentByIndex := make(map[int64]string, len(currentShards))
	for _, shard := range currentShards {
		currentByIndex[shard.Index.ValueInt64()] = shard.AvailabilityZone.ValueString()
	}

	plannedByIndex := make(map[int64]string, len(plannedShards))
	for _, shard := range plannedShards {
		plannedByIndex[shard.Index.ValueInt64()] = shard.AvailabilityZone.ValueString()
	}

	// Only compare shards that exist in both current and planned states.
	for idx, currentAZ := range currentByIndex {
		if plannedAZ, ok := plannedByIndex[idx]; ok {
			if currentAZ != plannedAZ {
				return true
			}
		}
	}
	return false
}

func determineDiff(currentState ValkeyClusterResourceModel, plan ValkeyClusterResourceModel) map[string]bool {
	diff := make(map[string]bool)
	if currentState.ShardCount.ValueInt64() != plan.ShardCount.ValueInt64() {
		diff["shard_count"] = true
	}
	if currentState.ReplicationFactor.ValueInt64() != plan.ReplicationFactor.ValueInt64() {
		diff["replication_factor"] = true
	}
	if currentState.NodeInstanceType.ValueString() != plan.NodeInstanceType.ValueString() {
		diff["node_instance_type"] = true
	}
	if currentState.EnforceShardMultiAz.ValueBool() != plan.EnforceShardMultiAz.ValueBool() {
		diff["enforce_shard_multi_az"] = true
	}
	if currentState.ShardPlacements == nil && plan.ShardPlacements != nil {
		diff["shard_placements"] = true
	} else if currentState.ShardPlacements != nil && plan.ShardPlacements == nil {
		diff["shard_placements"] = true
	} else if currentState.ShardPlacements != nil && plan.ShardPlacements != nil {
		if len(currentState.ShardPlacements) != len(plan.ShardPlacements) {
			diff["shard_placements"] = true
		} else {
			for i := range currentState.ShardPlacements {
				if currentState.ShardPlacements[i].Index.ValueInt64() != plan.ShardPlacements[i].Index.ValueInt64() ||
					currentState.ShardPlacements[i].AvailabilityZone.ValueString() != plan.ShardPlacements[i].AvailabilityZone.ValueString() ||
					!reflect.DeepEqual(currentState.ShardPlacements[i].ReplicaAvailabilityZones, plan.ShardPlacements[i].ReplicaAvailabilityZones) {
					diff["shard_placements"] = true
					break
				}
			}
		}
	}
	return diff
}

// POST /ec-cluster/<cluster-name>/replication-group
// Optional fields: node_instance_type, enforce_shard_multi_az
// Expected response: 202 Accepted.
func (r *ValkeyClusterResource) updateReplicationGroup(clusterName string, nodeInstanceType *string, enforceShardMultiAz *bool) error {
	requestMap := map[string]interface{}{}
	if nodeInstanceType != nil {
		requestMap["node_instance_type"] = *nodeInstanceType
	}
	if enforceShardMultiAz != nil {
		requestMap["enforce_shard_multi_az"] = *enforceShardMultiAz
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		return err
	}
	requestBody := bytes.NewBuffer(requestJson)

	client := *r.httpClient
	updateUrl := fmt.Sprintf("%s/ec-cluster/%s/replication-group", r.httpEndpoint, clusterName)
	updateRequest, err := http.NewRequest("POST", updateUrl, requestBody)
	if err != nil {
		return err
	}
	updateRequest.Header.Set("Authorization", r.httpAuthToken)
	updateRequest.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(updateRequest)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != 202 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("unable to update replication group, got non-202 response: %s %s", httpResp.Status, string(respBody))
	}
	return nil
}

// POST /ec-cluster/<cluster-name>/shard-configuration
// Required fields: shard_count, shards_to_remove (indexes)
// Expected response: 202 Accepted.
func (r *ValkeyClusterResource) decreaseShardCount(clusterName string, shardCount int, shardsToRemove []int) error {
	requestMap := map[string]interface{}{
		"shard_count":      shardCount,
		"shards_to_remove": shardsToRemove,
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		return err
	}
	requestBody := bytes.NewBuffer(requestJson)

	client := *r.httpClient
	updateUrl := fmt.Sprintf("%s/ec-cluster/%s/shard-configuration", r.httpEndpoint, clusterName)
	updateRequest, err := http.NewRequest("POST", updateUrl, requestBody)
	if err != nil {
		return err
	}
	updateRequest.Header.Set("Authorization", r.httpAuthToken)
	updateRequest.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(updateRequest)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != 202 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("unable to decrease shard count, got non-202 response: %s %s", httpResp.Status, string(respBody))
	}
	return nil
}

// POST /ec-cluster/<cluster-name>/shard-configuration
// Required fields: shard_count, shard_placements
// Expected response: 202 Accepted.
func (r *ValkeyClusterResource) increaseShardCount(clusterName string, shardCount int, shardPlacements []ShardPlacementModel) error {
	requestMap := map[string]interface{}{
		"shard_count":      shardCount,
		"shard_placements": shardPlacementsToAPIFormat(shardPlacements),
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		return err
	}
	requestBody := bytes.NewBuffer(requestJson)

	client := *r.httpClient
	updateUrl := fmt.Sprintf("%s/ec-cluster/%s/shard-configuration", r.httpEndpoint, clusterName)
	updateRequest, err := http.NewRequest("POST", updateUrl, requestBody)
	if err != nil {
		return err
	}
	updateRequest.Header.Set("Authorization", r.httpAuthToken)
	updateRequest.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(updateRequest)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != 202 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("unable to increase shard count, got non-202 response: %s %s", httpResp.Status, string(respBody))
	}
	return nil
}

// POST /ec-cluster/<cluster-name>/increase-replica-count
// Required fields: replication_factor
// Optional fields: shard_placements
// Expected response: 202 Accepted.
func (r *ValkeyClusterResource) increaseReplicaCount(clusterName string, replicationFactor int, shardPlacements []ShardPlacementModel) error {
	requestMap := map[string]interface{}{
		"replication_factor": replicationFactor,
	}
	if len(shardPlacements) > 0 {
		requestMap["shard_placements"] = shardPlacementsToAPIFormat(shardPlacements)
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		return err
	}
	requestBody := bytes.NewBuffer(requestJson)

	client := *r.httpClient
	updateUrl := fmt.Sprintf("%s/ec-cluster/%s/increase-replica-count", r.httpEndpoint, clusterName)
	updateRequest, err := http.NewRequest("POST", updateUrl, requestBody)
	if err != nil {
		return err
	}
	updateRequest.Header.Set("Authorization", r.httpAuthToken)
	updateRequest.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(updateRequest)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != 202 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("unable to increase replication factor, got non-202 response: %s %s", httpResp.Status, string(respBody))
	}
	return nil
}

// POST /ec-cluster/<cluster-name>/decrease-replica-count
// Required fields: replication_factor
// Optional fields: shard_placements
// Expected response: 202 Accepted.
func (r *ValkeyClusterResource) decreaseReplicaCount(clusterName string, replicationFactor int, shardPlacements []ShardPlacementModel) error {
	requestMap := map[string]interface{}{
		"replication_factor": replicationFactor,
	}
	if len(shardPlacements) > 0 {
		requestMap["shard_placements"] = shardPlacementsToAPIFormat(shardPlacements)
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		return err
	}
	requestBody := bytes.NewBuffer(requestJson)

	client := *r.httpClient
	updateUrl := fmt.Sprintf("%s/ec-cluster/%s/decrease-replica-count", r.httpEndpoint, clusterName)
	updateRequest, err := http.NewRequest("POST", updateUrl, requestBody)
	if err != nil {
		return err
	}
	updateRequest.Header.Set("Authorization", r.httpAuthToken)
	updateRequest.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(updateRequest)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != 202 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("unable to decrease replication factor, got non-202 response: %s %s", httpResp.Status, string(respBody))
	}
	return nil
}

func (r *ValkeyClusterResource) deleteClusterAndPollUntilGone(ctx context.Context, clusterName string) error {
	client := *r.httpClient
	deleteRequest, err := http.NewRequest("DELETE", fmt.Sprintf("%s/ec-cluster/%s", r.httpEndpoint, clusterName), nil)
	if err != nil {
		return err
	}
	deleteRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(deleteRequest)
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}
	if err == nil && httpResp != nil && httpResp.StatusCode == 404 {
		// If the cluster is already gone, no need to poll
		return nil
	}

	// Poll until the cluster is confirmed deleted (404)
	// There may be transient server errors during cluster deletion, so ignore non-404 errors and keep polling
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			describeRequest, err := http.NewRequest("GET", fmt.Sprintf("%s/ec-cluster/%s", r.httpEndpoint, clusterName), nil)
			if err != nil {
				continue
			}
			describeRequest.Header.Set("Authorization", r.httpAuthToken)
			describeResp, err := client.Do(describeRequest)
			if describeResp != nil && describeResp.Body != nil {
				_ = describeResp.Body.Close()
				if err == nil && describeResp.StatusCode == 404 {
					return nil
				}
			}
		}
	}
}

func (r *ValkeyClusterResource) pollUntilClusterReady(ctx context.Context, clusterName string, resp *resource.CreateResponse) {
	// Poll until cluster status is "Active" or "CreationFailed", log any other errors but do not stop polling
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Context has been cancelled, stop polling
			return
		case <-ticker.C:
			foundCluster, err := describeValkeyCluster(*r.httpClient, clusterName, r.httpEndpoint, r.httpAuthToken)
			if foundCluster != nil && foundCluster.Status == "Active" {
				return
			} else if foundCluster != nil && foundCluster.Status == "CreationFailed" {
				if err := r.deleteClusterAndPollUntilGone(ctx, clusterName); err != nil {
					resp.Diagnostics.AddError("Cluster Deletion Failed", fmt.Sprintf("Cluster \"%s\" failed to create and an attempt was made to delete it, but deletion failed with error: %s. You may need to manually delete the cluster before attempting another creation.", clusterName, err.Error()))
					return
				}
				resp.Diagnostics.AddError("Cluster Creation Failed", fmt.Sprintf("Cluster \"%s\" failed to create and has been deleted. Please try creating the resource again.", clusterName))
				resp.State.RemoveResource(ctx)
				return
			} else if err == nil && foundCluster == nil {
				// cluster not found, which could be a transient state during creation before the cluster is fully registered, keep polling
			} else if err != nil || foundCluster == nil {
				resp.Diagnostics.AddWarning("Describe after create error", fmt.Sprintf("Error: %s. Continuing to poll until cluster status is Active", err))
			}
		}
	}
}

func (r *ValkeyClusterResource) pollUntilClusterUpdated(ctx context.Context, clusterName string, resp *resource.UpdateResponse) {
	// Poll until cluster status is "Active", log any other errors but do not stop polling
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Context has been cancelled, stop polling
			return
		case <-ticker.C:
			foundCluster, err := describeValkeyCluster(*r.httpClient, clusterName, r.httpEndpoint, r.httpAuthToken)
			if foundCluster != nil && foundCluster.Status == "Active" {
				return
			} else if err != nil || foundCluster == nil {
				resp.Diagnostics.AddWarning("Describe after update error", fmt.Sprintf("Error: %s. Continuing to poll until cluster status is Active", err))
			}
		}

	}
}

func (r *ValkeyClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("cluster_name"), req, resp)
}

type DescribeValkeyClustersResponseData struct {
	Name                string `json:"name"`
	NodeInstanceType    string `json:"node_instance_type"`
	ShardCount          int64  `json:"shard_count"`
	ReplicationFactor   int64  `json:"replication_factor"`
	EnforceShardMultiAz bool   `json:"enforce_shard_multi_az"`
	ShardPlacements     []struct {
		ShardIndex               int64    `json:"shard_index"`
		AvailabilityZone         string   `json:"availability_zone"`
		ReplicaAvailabilityZones []string `json:"replica_availability_zones"`
	} `json:"shard_placements"`
	Status string   `json:"status"`
	Errors []string `json:"errors"`
}

func describeValkeyCluster(client http.Client, name string, httpEndpoint string, httpAuthToken string) (*DescribeValkeyClustersResponseData, error) {
	getRequest, err := http.NewRequest("GET", fmt.Sprintf("%s/ec-cluster/%s", httpEndpoint, name), nil)
	if err != nil {
		return nil, err
	}
	getRequest.Header.Set("Authorization", httpAuthToken)
	getResp, err := client.Do(getRequest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = getResp.Body.Close() }()
	// Do not error if 404 not found
	if getResp.StatusCode == 404 {
		return nil, nil
	}
	if getResp.StatusCode >= 300 {
		body, _ := io.ReadAll(getResp.Body)
		return nil, fmt.Errorf("unable to list valkey cluster, got non-200 response: %s %s", getResp.Status, string(body))
	}

	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var cluster DescribeValkeyClustersResponseData
	err = json.Unmarshal(bodyBytes, &cluster)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return &cluster, nil
}
