package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/momentohq/client-sdk-go/auth"
	"github.com/momentohq/client-sdk-go/config"
	"github.com/momentohq/client-sdk-go/momento"
)

// Ensure MomentoProvider satisfies various provider interfaces.
var _ provider.Provider = &MomentoProvider{}

// MomentoProvider defines the provider implementation.
type MomentoProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// MomentoProviderModel describes the provider data model.
type MomentoProviderModel struct {
	AuthToken types.String `tfsdk:"api_key"`
}

func (p *MomentoProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "momento"
	resp.Version = p.version
}

func (p *MomentoProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Momento API Key. May also be provided via MOMENTO_API_KEY environment variable.",
				Optional:            true,
			},
		},
	}
}

func (p *MomentoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	// Retrieve provider data from configuration
	var model MomentoProviderModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if model.AuthToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown Momento API Key",
			"The provider cannot create the Momento API client as there is an unknown configuration value for the Momento authentication token. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the MOMENTO_API_KEY environment variable.",
		)
	}

	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.

	authToken := os.Getenv("MOMENTO_API_KEY")

	if !model.AuthToken.IsNull() {
		authToken = model.AuthToken.ValueString()
	}

	// If any of the expected configurations are missing, return
	// errors with provider-specific guidance.

	if authToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Momento API Key",
			"The provider cannot create the Momento API client as there is a missing or empty value for the Momento authentication token. "+
				"Set the api_key value in the configuration or use the MOMENTO_API_KEY environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Create the Momento API client.

	credProvider, err := auth.NewStringMomentoTokenProvider(authToken)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Momento Token Provider",
			"An unexpected error occurred when creating the Momento API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Momento Client Error: "+err.Error(),
		)
		return
	}

	client, err := momento.NewCacheClient(config.LaptopLatest(), credProvider, 1)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Momento Cache Client",
			"An unexpected error occurred when creating the Momento API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Momento Client Error: "+err.Error(),
		)
		return
	}

	// Make the HashiCups client available during DataSource and Resource
	// type Configure methods.
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *MomentoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewCacheResource,
	}
}

func (p *MomentoProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCachesDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MomentoProvider{
			version: version,
		}
	}
}
