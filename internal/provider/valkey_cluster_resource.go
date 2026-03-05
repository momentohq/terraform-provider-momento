package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		err = httpResp.Body.Close()
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to close HTTP response body, got error: %s", err))
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create valkey cluster, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}

	// Map response body to schema and populate computed attribute values
	plan.Id = types.StringValue(plan.ClusterName.ValueString())

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	// Poll until cluster status is "Active"
	err = r.pollUntilClusterReady(plan.ClusterName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to poll cluster until creation confirmed, got error: %s", err))
		return
	}
}

func (r *ValkeyClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ValkeyClusterResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	deleteUrl := fmt.Sprintf("%s/ec-cluster/%s", r.httpEndpoint, state.ClusterName.ValueString())
	deleteRequest, err := http.NewRequest("DELETE", deleteUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to delete valkey cluster, got error: %s", err))
		return
	}
	deleteRequest.Header.Set("Authorization", r.httpAuthToken)

	httpResp, err := client.Do(deleteRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete valkey cluster, got error: %s", err))
		return
	}
	if httpResp.StatusCode == 404 {
		resp.Diagnostics.AddWarning("Client Warning", fmt.Sprintf("Valkey cluster with name \"%s\" not found, assuming already deleted", state.ClusterName.ValueString()))
		return
	}
	if httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete valkey cluster, got non-200 response: %d", httpResp.StatusCode))
		return
	}

	// Poll until cluster is deleted (no longer returned by list clusters call)
	foundCluster, err := findValkeyCluster(client, state.ClusterName.ValueString(), r.httpEndpoint, r.httpAuthToken)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to list valkey clusters to confirm deletion, got error: %s", err))
		return
	}
	if foundCluster == nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Cluster with name \"%s\" not found, unable to poll until confirmed deletion", state.ClusterName.ValueString()))
		return
	}
	for foundCluster != nil {
		time.Sleep(1 * time.Minute)
		foundCluster, err = findValkeyCluster(client, state.ClusterName.ValueString(), r.httpEndpoint, r.httpAuthToken)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to list valkey clusters to confirm deletion, got error: %s", err))
			return
		}
		if foundCluster == nil {
			return
		}
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
	foundCluster, err := findValkeyCluster(client, state.ClusterName.ValueString(), r.httpEndpoint, r.httpAuthToken)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to list valkey clusters, got error: %s", err))
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
				var replicaAZs []types.String
				for _, az := range sp.ReplicaAvailabilityZones {
					replicaAZs = append(replicaAZs, types.StringValue(az))
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
	var state ValkeyClusterResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning("Internal Error", "Valkey Cluster resource does not yet support updates, please delete and recreate the resource to make changes")
}

func (r *ValkeyClusterResource) pollUntilClusterReady(clusterName string) error {
	// Poll until cluster status is "Active"
	foundCluster, err := findValkeyCluster(*r.httpClient, clusterName, r.httpEndpoint, r.httpAuthToken)
	if err != nil {
		return err
	}
	if foundCluster == nil {
		return fmt.Errorf("cluster with name \"%s\" not found", clusterName)
	}
	for foundCluster.Status != "Active" {
		// wait 1 minute
		time.Sleep(1 * time.Minute)
		foundCluster, err = findValkeyCluster(*r.httpClient, clusterName, r.httpEndpoint, r.httpAuthToken)
		if err != nil {
			return err
		}
		if foundCluster == nil {
			return fmt.Errorf("cluster with name \"%s\" not found", clusterName)
		}
	}
	return nil
}

func (r *ValkeyClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
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

func findValkeyCluster(client http.Client, name string, httpEndpoint string, httpAuthToken string) (*DescribeValkeyClustersResponseData, error) {
	getRequest, err := http.NewRequest("GET", fmt.Sprintf("%s/ec-cluster/%s", httpEndpoint, name), nil)
	if err != nil {
		return nil, err
	}
	getRequest.Header.Set("Authorization", httpAuthToken)
	getResp, err := client.Do(getRequest)
	if err != nil {
		return nil, err
	}
	if getResp.StatusCode == 404 {
		return nil, nil
	}
	if getResp.StatusCode >= 300 {
		body, _ := io.ReadAll(getResp.Body)
		err = getResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("unable to close HTTP response body, got error: %s", err)
		}
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
