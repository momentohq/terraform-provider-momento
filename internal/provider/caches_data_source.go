package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/momentohq/client-sdk-go/momento"
	"github.com/momentohq/client-sdk-go/responses"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ datasource.DataSource              = &CachesDataSource{}
	_ datasource.DataSourceWithConfigure = &CachesDataSource{}
)

func NewCachesDataSource() datasource.DataSource {
	return &CachesDataSource{}
}

// CachesDataSource defines the data source implementation.
type CachesDataSource struct {
	client *momento.CacheClient
}

// CachesDataSourceModel describes the data source data model.
type CachesDataSourceModel struct {
	Id     types.String                 `tfsdk:"id"`
	Caches []CachesDataSourceCacheModel `tfsdk:"caches"`
}

type CachesDataSourceCacheModel struct {
	Name types.String `tfsdk:"name"`
}

func (d *CachesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_caches"
}

func (d *CachesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A list of Momento serverless caches.",

		Attributes: map[string]schema.Attribute{
			// The testing framework requires an id attribute to be present in every data source and resource
			"id": schema.StringAttribute{
				Description: "Placeholder identifier attribute.",
				Computed:    true,
			},
			"caches": schema.ListNestedAttribute{
				Description: "List of caches.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "Name of the cache.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *CachesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(momento.CacheClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected momento.CacheClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = &client
}

func (d *CachesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data CachesDataSourceModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var caches []string

	// Retrieve data from the API
	var client momento.CacheClient = *d.client
	caches, err := listCaches(ctx, client)
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to list caches, got error: %s", err.Error()),
		)
		return
	}

	// Save data into the model
	for _, cache := range caches {
		cacheState := CachesDataSourceCacheModel{
			Name: types.StringValue(cache),
		}
		data.Caches = append(data.Caches, cacheState)
	}

	data.Id = types.StringValue("placeholder")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func listCaches(ctx context.Context, client momento.CacheClient) ([]string, error) {
	var caches []string
	resp, err := client.ListCaches(ctx, &momento.ListCachesRequest{})
	if err != nil {
		return nil, err
	}
	if r, ok := resp.(*responses.ListCachesSuccess); ok {
		for _, cacheInfo := range r.Caches() {
			caches = append(caches, cacheInfo.Name())
		}
	} else {
		return nil, fmt.Errorf("unexpected response type %T", resp)
	}
	return caches, nil
}
