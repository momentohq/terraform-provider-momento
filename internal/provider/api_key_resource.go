package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource               = &ApiKeyResource{}
	_ resource.ResourceWithConfigure  = &ApiKeyResource{}
	_ resource.ResourceWithModifyPlan = &ApiKeyResource{}
)

func NewApiKeyResource() resource.Resource {
	return &ApiKeyResource{}
}

// ApiKeyResource defines the resource implementation.
type ApiKeyResource struct {
	httpClient      *http.Client
	httpMgaEndpoint string
	httpAuthToken   string
}

// ApiKeyResourceModel describes the resource data model.
type ApiKeyResourceModel struct {
	KeyId               types.String      `tfsdk:"key_id"`
	ApiKey              types.String      `tfsdk:"api_key"`
	RefreshToken        types.String      `tfsdk:"refresh_token"`
	RoleId              types.String      `tfsdk:"role_id"`
	Description         types.String      `tfsdk:"description"`
	Expiry              timetypes.RFC3339 `tfsdk:"expiry"`
	ExcludeRefreshToken types.Bool        `tfsdk:"exclude_refresh_token"`
}

func (r *ApiKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *ApiKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An API Key.",

		Attributes: map[string]schema.Attribute{
			"key_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the API Key.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "The plaintext API Key.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"refresh_token": schema.StringAttribute{
				MarkdownDescription: "The single-use refresh token for rotating the API Key.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the API Key's role.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the API Key is for.",
				Required:            true,
			},
			"expiry": schema.StringAttribute{
				MarkdownDescription: "When the key should expire, in RFC 3339 (ISO) format, e.g. `2030-01-01T00:00:00Z`. If omitted, the key never expires.",
				Optional:            true,
				CustomType:          timetypes.RFC3339Type{},
			},
			"exclude_refresh_token": schema.BoolAttribute{
				MarkdownDescription: "Set to true to generate the key without a refresh token. Keys without an expiry are never given a refresh token.",
				Optional:            true,
			},
		},
	}
}

func (r *ApiKeyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	if !clients.usingV2ApiKey {
		resp.Diagnostics.AddError(
			"Momento V2 API Key Required",
			"The momento_api_key resource requires a V2 API key."+
				" Set v2_api_key and v2_api_endpoint in the provider configuration,"+
				" or set MOMENTO_API_KEY alongside MOMENTO_ENDPOINT in your environment variables.",
		)

		return
	}

	r.httpClient = clients.httpClient
	r.httpMgaEndpoint = clients.httpMgaEndpoint
	r.httpAuthToken = clients.httpAuthToken
}

type ApiKeyInfo struct {
	KeyId       string `json:"key_id"`
	AccountId   string `json:"account_id"`
	Description string `json:"description"`
	RoleId      string `json:"role_id"`
	ExpiresAt   *int64 `json:"expires_at_epoch_seconds"`
	IssuedAt    int64  `json:"issued_at_epoch_seconds"`
}

type ApiKeyResponse struct {
	ApiKey       string     `json:"api_key"`
	RefreshToken *string    `json:"refresh_token"`
	KeyInfo      ApiKeyInfo `json:"key_info"`
}

func (r *ApiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApiKeyResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	postUrl := fmt.Sprintf("%s/api-keys", r.httpMgaEndpoint)

	requestMap := map[string]any{
		"description": plan.Description.ValueString(),
		"role_id":     plan.RoleId.ValueString(),
	}
	if plan.Expiry.IsNull() {
		requestMap["expiry"] = "never"
	} else {
		expiry, diags := validateExpiry(plan.Expiry)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		requestMap["expiry"] = expiry.Unix()
	}
	if !plan.ExcludeRefreshToken.IsNull() {
		requestMap["exclude_refresh_token"] = plan.ExcludeRefreshToken.ValueBool()
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal request body, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)
	postRequest, err := http.NewRequest("POST", postUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to generate API key, got error: %s", err))
		return
	}
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(postRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}

	const orphanWarning = "The API key may have been created but could not be tracked by Terraform; revoke it manually if it exists."
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to generate API key, got error: %s. %s", err, orphanWarning))
		return
	}
	body, readErr := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode >= 300 {
		msg := fmt.Sprintf("Unable to generate API key, got non-200 response: %s %s", httpResp.Status, string(body))
		if httpResp.StatusCode >= 500 {
			msg += ". " + orphanWarning
		}
		resp.Diagnostics.AddError("Client Error", msg)
		return
	}
	if readErr != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf(
			"Unable to read response from generating API key, got error: %s. %s", readErr, orphanWarning))
		return
	}

	// Map response body to schema and populate computed attribute values
	var apiKey ApiKeyResponse
	err = json.Unmarshal(body, &apiKey)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to unmarshal JSON response from generating API key, got error: %v. %s", err, orphanWarning))
		return
	}
	if apiKey.KeyInfo.KeyId == "" {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Response from generating API key did not include a key ID. %s", orphanWarning))
		return
	}

	plan.KeyId = types.StringValue(apiKey.KeyInfo.KeyId)
	plan.ApiKey = types.StringValue(apiKey.ApiKey)
	plan.RefreshToken = types.StringPointerValue(apiKey.RefreshToken)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ApiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApiKeyResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	deleteUrl := fmt.Sprintf("%s/api-keys/%s", r.httpMgaEndpoint, state.KeyId.ValueString())
	deleteRequest, err := http.NewRequest("DELETE", deleteUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to revoke API key, got error: %s", err))
		return
	}
	deleteRequest.Header.Set("Authorization", r.httpAuthToken)

	httpResp, err := client.Do(deleteRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to revoke API key, got error: %s", err))
		return
	}
	if httpResp.StatusCode == 404 {
		// Already deleted
		return
	}
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to revoke API key, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}
}

func (r *ApiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApiKeyResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Find API key
	client := *r.httpClient
	foundApiKeyInfo, err := describeApiKey(client, state.KeyId.ValueString(), r.httpMgaEndpoint, r.httpAuthToken)
	if foundApiKeyInfo == nil && err == nil {
		// API key not found, remove from state
		resp.Diagnostics.AddWarning("API Key Not Found", fmt.Sprintf("The API key with ID \"%s\" was not found. It may have been deleted outside of Terraform. Removing from state.", state.KeyId.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read API key, got error: %s", err))
		return
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ApiKeyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan ApiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if req.State.Raw.IsNull() { // on Create:
		if !plan.Expiry.IsNull() && !plan.Expiry.IsUnknown() {
			_, diags := validateExpiry(plan.Expiry)
			resp.Diagnostics.Append(diags...)
		}
	} else { // on Update:
		var state ApiKeyResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if !plan.RoleId.Equal(state.RoleId) || !plan.Description.Equal(state.Description) || !plan.Expiry.Equal(state.Expiry) || !plan.ExcludeRefreshToken.Equal(state.ExcludeRefreshToken) {
			resp.Diagnostics.AddWarning(
				"API Key Cannot Be Updated",
				fmt.Sprintf(
					"API Key resource does not support updates, so applying this plan will fail for key %s."+
						" Instead, please revoke/destroy your key when you're ready and generate a new one.",
					state.KeyId.ValueString()))
		}
	}
}

func validateExpiry(expiry timetypes.RFC3339) (time.Time, diag.Diagnostics) {
	expiryTime, diags := expiry.ValueRFC3339Time()
	if diags.HasError() {
		return expiryTime, diags
	}
	if !expiryTime.After(time.Now()) {
		diags.AddAttributeError(
			path.Root("expiry"),
			"Invalid Expiry",
			fmt.Sprintf("Expiry is in the past: %s", expiry.ValueString()),
		)
	}
	return expiryTime, diags
}

func (r *ApiKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("API Key Cannot Be Updated", "API Key resource does not support updates. Instead, please revoke/destroy your key when you're ready and generate a new one.")
}

type ListApiKeysResponse struct {
	KeyInfo   []ApiKeyInfo `json:"key_info"`
	NextToken *string      `json:"next_token"`
}

func listApiKeys(client http.Client, httpMgaEndpoint string, httpAuthToken string, nextToken *string) (*ListApiKeysResponse, error) {
	getUrl := fmt.Sprintf("%s/api-keys", httpMgaEndpoint)
	if nextToken != nil {
		getUrl = fmt.Sprintf("%s?next_token=%v", getUrl, *nextToken)
	}
	getRequest, err := http.NewRequest("GET", getUrl, nil)
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
	if getResp.StatusCode >= 300 {
		body, _ := io.ReadAll(getResp.Body)
		return nil, fmt.Errorf("unable to list API keys, got non-2xx response: %s %s", getResp.Status, string(body))
	}

	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	var apiKeysList ListApiKeysResponse
	err = json.Unmarshal(bodyBytes, &apiKeysList)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return &apiKeysList, nil
}

func describeApiKey(client http.Client, keyId string, httpMgaEndpoint string, httpAuthToken string) (*ApiKeyInfo, error) {
	var nextToken *string
	for {
		apiKeysList, err := listApiKeys(client, httpMgaEndpoint, httpAuthToken, nextToken)
		if err != nil {
			return nil, fmt.Errorf("error listing API keys: %v", err)
		}
		for _, key := range apiKeysList.KeyInfo {
			if key.KeyId == keyId {
				return &key, nil
			}
		}
		nextToken = apiKeysList.NextToken
		if nextToken == nil {
			break
		}
	}
	return nil, nil
}
