package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource               = &ValkeyDatabaseResource{}
	_ resource.ResourceWithConfigure  = &ValkeyDatabaseResource{}
	_ resource.ResourceWithModifyPlan = &ValkeyDatabaseResource{}
)

func NewValkeyDatabaseResource() resource.Resource {
	return &ValkeyDatabaseResource{}
}

// ValkeyDatabaseResource defines the resource implementation.
type ValkeyDatabaseResource struct {
	httpClient    *http.Client
	httpEndpoint  string
	httpAuthToken string
}

// ValkeyDatabaseResourceModel describes the resource data model.
type ValkeyDatabaseResourceModel struct {
	Name     types.String `tfsdk:"name"`
	PoolName types.String `tfsdk:"pool_name"`
}

func (r *ValkeyDatabaseResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_valkey_database"
}

func (r *ValkeyDatabaseResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Valkey Database.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the Valkey Database.",
				Required:            true,
			},
			"pool_name": schema.StringAttribute{
				MarkdownDescription: "The name of the Capacity Pool that this Database is pinned to.",
				Required:            true,
			},
		},
	}
}

func (r *ValkeyDatabaseResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

type ValkeyDatabaseData struct {
	Name     string `json:"name"`
	PoolName string `json:"pool_name"`
}

func (r *ValkeyDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ValkeyDatabaseResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	postUrl := fmt.Sprintf("%s/database/%s", r.httpEndpoint, plan.Name.ValueString())

	requestMap := map[string]any{
		"name":      plan.Name.ValueString(),
		"pool_name": plan.PoolName.ValueString(),
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal request body, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)
	postRequest, err := http.NewRequest("POST", postUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to create Valkey Database, got error: %s", err))
		return
	}
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(postRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create Valkey Database, got error: %s", err))
		return
	}
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		msg := fmt.Sprintf("Unable to create Valkey Database, got non-200 response: %s %s", httpResp.Status, string(body))
		resp.Diagnostics.AddError("Client Error", msg)
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ValkeyDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ValkeyDatabaseResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	deleteUrl := fmt.Sprintf("%s/database/%s", r.httpEndpoint, state.Name.ValueString())
	deleteRequest, err := http.NewRequest("DELETE", deleteUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to delete Valkey Database, got error: %s", err))
		return
	}
	deleteRequest.Header.Set("Authorization", r.httpAuthToken)

	httpResp, err := client.Do(deleteRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete Valkey Database, got error: %s", err))
		return
	}
	if httpResp.StatusCode == 404 {
		// Already deleted
		return
	}
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete Valkey Database, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}
}

func (r *ValkeyDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ValkeyDatabaseResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Find Valkey Database
	client := *r.httpClient
	foundDatabase, err := describeValkeyDatabase(ctx, client, state.Name.ValueString(), r.httpEndpoint, r.httpAuthToken)
	if foundDatabase == nil && err == nil {
		// Valkey database not found, remove from state
		resp.Diagnostics.AddWarning("Valkey Database Not Found", fmt.Sprintf("The Valkey Database \"%s\" was not found. It may have been deleted outside of Terraform. Removing from state.", state.Name.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read Valkey Database, got error: %s", err))
		return
	}
	if foundDatabase == nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read Valkey Database, name \"%s\" not found", state.Name.ValueString()))
		return
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

const databaseUpdateTip = "Instead, please create a new Database, move your resources, and destroy the old Database when you're ready."

func (r *ValkeyDatabaseResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Only run during updates: skip Create (state null), Delete (plan null), or unconfigured provider.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || r.httpClient == nil {
		return
	}

	var plan, state ValkeyDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Name.Equal(state.Name) || !plan.PoolName.Equal(state.PoolName) {
		resp.Diagnostics.AddWarning(
			"Valkey Database Cannot Be Updated",
			fmt.Sprintf(
				"Valkey Database resource does not support updates, so applying this plan will fail for Database %s. %s",
				state.Name.ValueString(), databaseUpdateTip))
	}
}

func (r *ValkeyDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Valkey Database Cannot Be Updated",
		fmt.Sprintf("Valkey Database resource does not support updates. %s", databaseUpdateTip))
}

func describeValkeyDatabase(ctx context.Context, client http.Client, name string, httpEndpoint string, httpAuthToken string) (*ValkeyDatabaseData, error) {
	getRequest, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/database/%s", httpEndpoint, name), nil)
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
		return nil, fmt.Errorf("unable to describe Valkey Database, got non-200 response: %s %s", getResp.Status, string(body))
	}

	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var database ValkeyDatabaseData
	err = json.Unmarshal(bodyBytes, &database)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return &database, nil
}
