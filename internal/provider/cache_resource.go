package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/momentohq/client-sdk-go/momento"
	"github.com/momentohq/client-sdk-go/responses"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &CacheResource{}
	_ resource.ResourceWithConfigure   = &CacheResource{}
	_ resource.ResourceWithImportState = &CacheResource{}
)

func NewCacheResource() resource.Resource {
	return &CacheResource{}
}

// CacheResource defines the resource implementation.
type CacheResource struct {
	client *momento.CacheClient
}

// CacheResourceModel describes the resource data model.
type CacheResourceModel struct {
	Name types.String `tfsdk:"name"`
	Id   types.String `tfsdk:"id"`
}

func (r *CacheResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cache"
}

func (r *CacheResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Momento serverless cache.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the cache.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			// The testing framework requires an id attribute to be present in every data source and resource
			"id": schema.StringAttribute{
				Description: "The ID of the cache.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *CacheResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	client := clients.cache

	r.client = &client
}

func (r *CacheResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CacheResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Create new cache
	client := *r.client
	createResp, err := client.CreateCache(ctx, &momento.CreateCacheRequest{
		CacheName: plan.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create cache, got error: %s", err))
		return
	}

	switch createResp.(type) {
	case *responses.CreateCacheSuccess:
		break
	case *responses.CreateCacheAlreadyExists:
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create cache, cache with name \"%s\" already exists", plan.Name.ValueString()))
		return
	default:
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create cache, got unknown response type: %T", createResp))
		return
	}

	// Map response body to schema and populate computed attribute values
	plan.Id = types.StringValue(plan.Name.ValueString())

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CacheResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CacheResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Find cache
	client := *r.client
	found, err := findCache(ctx, client, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to list caches, got error: %s", err))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read cache, cache with name \"%s\" not found", state.Name.ValueString()))
		return
	}

	state.Id = types.StringValue(state.Name.ValueString())

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *CacheResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state CacheResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddError("Internal Error", "Cache resource does not support updates")
}

func (r *CacheResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CacheResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Delete cache
	client := *r.client
	deleteResp, err := client.DeleteCache(ctx, &momento.DeleteCacheRequest{
		CacheName: state.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete cache, got error: %s", err))
		return
	}

	switch deleteResp.(type) {
	case *responses.DeleteCacheSuccess:
		break
	default:
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete cache, got unknown response type: %T", deleteResp))
		return
	}
}

func (r *CacheResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func findCache(ctx context.Context, client momento.CacheClient, name string) (bool, error) {
	resp, err := client.ListCaches(ctx, &momento.ListCachesRequest{})
	if err != nil {
		return false, err
	}
	if r, ok := resp.(*responses.ListCachesSuccess); ok {
		for _, cacheInfo := range r.Caches() {
			if cacheInfo.Name() == name {
				return true, nil
			}
		}
	} else {
		return false, fmt.Errorf("unexpected response type %T", resp)
	}
	return false, nil
}
