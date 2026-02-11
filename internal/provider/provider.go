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
	V2ApiKey  types.String `tfsdk:"v2_api_key"`
	Endpoint  types.String `tfsdk:"v2_api_endpoint"`
}

type MomentoClients struct {
	cache       momento.CacheClient
	leaderboard momento.PreviewLeaderboardClient
}

func (p *MomentoProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "momento"
	resp.Version = p.version
}

func (p *MomentoProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Momento disposable token or legacy API key. May also be provided via MOMENTO_API_KEY environment variable. Do NOT set the MOMENTO_ENDPOINT environment variable if you are using a disposable token or legacy API key.",
				Optional:            true,
			},
			"v2_api_key": schema.StringAttribute{
				MarkdownDescription: "Momento V2 API Key. May also be provided via MOMENTO_API_KEY environment variable alongside the MOMENTO_ENDPOINT environment variable.",
				Optional:            true,
			},
			"v2_api_endpoint": schema.StringAttribute{
				MarkdownDescription: "Momento API Endpoint. May also be provided via MOMENTO_ENDPOINT environment variable alongside the MOMENTO_API_KEY environment variable containing a V2 API key.",
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
			"Unknown Momento disposable token or legacy API key value",
			"The provider cannot create the Momento client as there is an unknown configuration value for the Momento disposable token or legacy API key field. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the MOMENTO_API_KEY environment variable.",
		)
	}

	if model.V2ApiKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("v2_api_key"),
			"Unknown Momento V2 API key value",
			"The provider cannot create the Momento client because there is an unknown configuration value for the Momento V2 API key field. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the MOMENTO_API_KEY environment variable alongside the MOMENTO_ENDPOINT environment variable.",
		)
	}

	if model.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("v2_api_endpoint"),
			"Unknown Momento V2 API endpoint value",
			"The provider cannot create the Momento client as there is an unknown configuration value for the Momento V2 API endpoint field. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the MOMENTO_ENDPOINT environment variable alongside the MOMENTO_API_KEY environment variable storing a v2 API key.",
		)
	}

	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.

	authToken := os.Getenv("MOMENTO_API_KEY")
	v2ApiKey := os.Getenv("MOMENTO_API_KEY")
	endpoint := os.Getenv("MOMENTO_ENDPOINT")

	if !model.AuthToken.IsNull() {
		authToken = model.AuthToken.ValueString()
	}
	if !model.V2ApiKey.IsNull() {
		v2ApiKey = model.V2ApiKey.ValueString()
	}
	if !model.Endpoint.IsNull() {
		endpoint = model.Endpoint.ValueString()
	}

	// If endpoint is present, assume we're using v2 api key, so both variables must be set.
	// Otherwise default to using disposable token or legacy API key.
	if endpoint != "" && v2ApiKey == "" {
		resp.Diagnostics.AddError(
			"Missing Momento V2 API Key",
			"The provider cannot create the Momento API client as there is a missing or empty value for the Momento V2 API key. "+
				"Set the v2_api_key value in the configuration or use the MOMENTO_API_KEY environment variable alongside the MOMENTO_ENDPOINT environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Use the appropriate credential provider based on the provided credentials.
	var credProvider auth.CredentialProvider
	var credError error

	if endpoint != "" && v2ApiKey != "" {
		credProvider, credError = auth.FromApiKeyV2(auth.ApiKeyV2Props{ApiKey: v2ApiKey, Endpoint: endpoint})
		if credError != nil {
			resp.Diagnostics.AddError(
				"Unable to Create Momento Credential Provider using FromApiKeyV2",
				"An unexpected error occurred when creating the Momento Credential Provider. "+
					"If the error is not clear, please contact the provider developers.\n\n"+
					"Momento Client Error: "+credError.Error(),
			)
			return
		}
	} else {
		credProvider, credError = auth.FromDisposableToken(authToken)
		if credError != nil {
			resp.Diagnostics.AddError(
				"Unable to Create Momento Credential Provider using FromDisposableToken",
				"An unexpected error occurred when creating the Momento Credential Provider. "+
					"If the error is not clear, please contact the provider developers.\n\n"+
					"Momento Client Error: "+credError.Error(),
			)
			return
		}
	}

	// Create the Momento API client.

	cacheClient, err := momento.NewCacheClient(config.LaptopLatest(), credProvider, 1)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Momento Cache Client",
			"An unexpected error occurred when creating the Momento API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Momento Client Error: "+err.Error(),
		)
		return
	}

	leaderboardClient, err := momento.NewPreviewLeaderboardClient(config.LeaderboardDefault(), credProvider)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Momento Leaderboard Client",
			"An unexpected error occurred when creating the Momento API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Momento Client Error: "+err.Error(),
		)
		return
	}

	// Make the Momento client available during DataSource and Resource
	// type Configure methods.
	resp.DataSourceData = MomentoClients{
		cache:       cacheClient,
		leaderboard: leaderboardClient,
	}
	resp.ResourceData = MomentoClients{
		cache:       cacheClient,
		leaderboard: leaderboardClient,
	}
}

func (p *MomentoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewCacheResource,
		NewLeaderboardResource,
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
