package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/momentohq/client-sdk-go/momento"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource              = &LeaderboardResource{}
	_ resource.ResourceWithConfigure = &LeaderboardResource{}
)

func NewLeaderboardResource() resource.Resource {
	return &LeaderboardResource{}
}

// LeaderboardResource defines the resource implementation.
type LeaderboardResource struct {
	client *momento.PreviewLeaderboardClient
}

// LeaderboardResourceModel describes the resource data model.
type LeaderboardResourceModel struct {
	Name      types.String `tfsdk:"name"`
	CacheName types.String `tfsdk:"cache_name"`
	Id        types.String `tfsdk:"id"`
}

func (l *LeaderboardResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_leaderboard"
}

func (l *LeaderboardResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Momento serverless leaderboard.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the leaderboard.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cache_name": schema.StringAttribute{
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

func (l *LeaderboardResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	client := clients.leaderboard

	l.client = &client
}

func (l *LeaderboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LeaderboardResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Create new Leaderboard
	client := *l.client
	_, err := client.Leaderboard(ctx, &momento.LeaderboardRequest{
		LeaderboardName: plan.Name.ValueString(),
		CacheName:       plan.CacheName.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create leaderboard, got error: %s", err))
		return
	}

	// Map response body to schema and populate computed attribute values
	plan.Id = types.StringValue(plan.Name.ValueString())

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (l *LeaderboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LeaderboardResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Close the Leaderboard
	client := *l.client
	client.Close()
}

func (l *LeaderboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LeaderboardResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	state.Id = types.StringValue(state.Name.ValueString())

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}
}

func (l *LeaderboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state LeaderboardResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddError("Internal Error", "Leaderboard resource does not support updates")
}
