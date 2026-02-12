package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource              = &ObjectStoreResource{}
	_ resource.ResourceWithConfigure = &ObjectStoreResource{}
)

func NewObjectStoreResource() resource.Resource {
	return &ObjectStoreResource{}
}

// ObjectStoreResource defines the resource implementation.
type ObjectStoreResource struct {
	httpClient    *http.Client
	httpEndpoint  string
	httpAuthToken string
}

type AccessLoggingConfig struct {
	Region       types.String `tfsdk:"region"`
	IamRoleArn   types.String `tfsdk:"iam_role_arn"`
	LogGroupName types.String `tfsdk:"log_group_name"`
}

// ObjectStoreResourceModel describes the resource data model.
type ObjectStoreResourceModel struct {
	Id                  types.String         `tfsdk:"id"`
	Name                types.String         `tfsdk:"name"`
	S3BucketName        types.String         `tfsdk:"s3_bucket_name"`
	S3Prefix            types.String         `tfsdk:"s3_prefix"`
	S3IamRoleArn        types.String         `tfsdk:"s3_iam_role_arn"`
	ValkeyClusterName   types.String         `tfsdk:"valkey_cluster_name"`
	AccessLoggingConfig *AccessLoggingConfig `tfsdk:"access_logging_config"`
}

func (r *ObjectStoreResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_store"
}

func (r *ObjectStoreResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An Object Store.",

		Attributes: map[string]schema.Attribute{
			// The testing framework requires an id attribute to be present in every data source and resource
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the Object Store.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the Object Store.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"s3_bucket_name": schema.StringAttribute{
				MarkdownDescription: "Name of the S3 bucket for the Object Store.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"s3_prefix": schema.StringAttribute{
				MarkdownDescription: "Optional prefix path within the S3 bucket.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"s3_iam_role_arn": schema.StringAttribute{
				MarkdownDescription: "The ARN of the IAM role that Momento will assume to access your S3 bucket.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"valkey_cluster_name": schema.StringAttribute{
				MarkdownDescription: "The name of the Momento Valkey Cluster to use for automatic caching.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"access_logging_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Optional configuration for access logging through CloudWatch.",
				Optional:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"region": schema.StringAttribute{
						MarkdownDescription: "The AWS region where the CloudWatch log group is located.",
						Required:            true,
					},
					"iam_role_arn": schema.StringAttribute{
						MarkdownDescription: "The ARN of the IAM role that Momento will assume to write logs.",
						Required:            true,
					},
					"log_group_name": schema.StringAttribute{
						MarkdownDescription: "The CloudWatch Log Group name where access logs will be delivered. The log group must already exist.",
						Required:            true,
					},
				},
			},
		},
	}
}

func (r *ObjectStoreResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ObjectStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ObjectStoreResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If required fields are missing values, return error
	if plan.Name.IsNull() || plan.Name.IsUnknown() || plan.Name.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Missing required value",
			"The Object Store name is required.",
		)
		return
	}
	if plan.S3BucketName.IsNull() || plan.S3BucketName.IsUnknown() || plan.S3BucketName.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("s3_bucket_name"),
			"Missing required value",
			"The S3 bucket name is required.",
		)
		return
	}
	if plan.S3IamRoleArn.IsNull() || plan.S3IamRoleArn.IsUnknown() || plan.S3IamRoleArn.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("s3_iam_role_arn"),
			"Missing required value",
			"The S3 IAM Role ARN is required.",
		)
		return
	}
	if plan.ValkeyClusterName.IsNull() || plan.ValkeyClusterName.IsUnknown() || plan.ValkeyClusterName.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("valkey_cluster_name"),
			"Missing required value",
			"The Valkey Cluster name is required.",
		)
		return
	}
	if plan.AccessLoggingConfig != nil {
		if plan.AccessLoggingConfig.LogGroupName.IsNull() || plan.AccessLoggingConfig.LogGroupName.IsUnknown() || plan.AccessLoggingConfig.LogGroupName.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(
				path.Root("access_logging_config").AtName("log_group_name"),
				"Missing required value",
				"The CloudWatch Log Group name is required when access logging config is set.",
			)
			return
		}
		if plan.AccessLoggingConfig.IamRoleArn.IsNull() || plan.AccessLoggingConfig.IamRoleArn.IsUnknown() || plan.AccessLoggingConfig.IamRoleArn.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(
				path.Root("access_logging_config").AtName("iam_role_arn"),
				"Missing required value",
				"The IAM Role ARN is required when access logging config is set.",
			)
			return
		}
		if plan.AccessLoggingConfig.Region.IsNull() || plan.AccessLoggingConfig.Region.IsUnknown() || plan.AccessLoggingConfig.Region.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(
				path.Root("access_logging_config").AtName("region"),
				"Missing required value",
				"The AWS region is required when access logging config is set.",
			)
			return
		}
	}

	client := *r.httpClient

	// Create map of request body to marshal into JSON
	requestData := DescribeObjectStoresResponseData{
		Name: plan.Name.ValueString(),
		StorageConfig: struct {
			S3 struct {
				BucketName string `json:"bucket_name"`
				Prefix     string `json:"prefix"`
				IamRoleArn string `json:"iam_role_arn"`
			} `json:"s3"`
		}{
			S3: struct {
				BucketName string `json:"bucket_name"`
				Prefix     string `json:"prefix"`
				IamRoleArn string `json:"iam_role_arn"`
			}{
				BucketName: plan.S3BucketName.ValueString(),
				Prefix:     plan.S3Prefix.ValueString(),
				IamRoleArn: plan.S3IamRoleArn.ValueString(),
			},
		},
		CacheConfig: struct {
			ValkeyCluster struct {
				ClusterName string `json:"cluster_name"`
			} `json:"valkey_cluster"`
		}{
			ValkeyCluster: struct {
				ClusterName string `json:"cluster_name"`
			}{
				ClusterName: plan.ValkeyClusterName.ValueString(),
			},
		},
	}
	if plan.AccessLoggingConfig != nil {
		requestData.AccessLoggingConfig = struct {
			Cloudwatch struct {
				LogGroupName string `json:"log_group_name"`
				IamRoleArn   string `json:"iam_role_arn"`
				Region       string `json:"region"`
			} `json:"cloudwatch"`
		}{
			Cloudwatch: struct {
				LogGroupName string `json:"log_group_name"`
				IamRoleArn   string `json:"iam_role_arn"`
				Region       string `json:"region"`
			}{
				LogGroupName: plan.AccessLoggingConfig.LogGroupName.ValueString(),
				IamRoleArn:   plan.AccessLoggingConfig.IamRoleArn.ValueString(),
				Region:       plan.AccessLoggingConfig.Region.ValueString(),
			},
		}
	}
	requestJson, err := json.Marshal(requestData)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal object store request to JSON, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)

	putUrl := fmt.Sprintf("%s/objectstore/%s", r.httpEndpoint, plan.Name.ValueString())
	putRequest, err := http.NewRequest("PUT", putUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to create object store, got error: %s", err))
		return
	}
	putRequest.Header.Set("Content-Type", "application/json")
	putRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(putRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create object store, got error: %s", err))
		return
	}
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		err = httpResp.Body.Close()
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to close HTTP response body, got error: %s", err))
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create object store, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}

	// Map response body to schema and populate computed attribute values
	plan.Id = types.StringValue(plan.Name.ValueString())

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ObjectStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ObjectStoreResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	deleteUrl := fmt.Sprintf("%s/objectstore/%s", r.httpEndpoint, state.Name.ValueString())
	deleteRequest, err := http.NewRequest("DELETE", deleteUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to delete object store, got error: %s", err))
		return
	}
	deleteRequest.Header.Set("Authorization", r.httpAuthToken)

	httpResp, err := client.Do(deleteRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete object store, got error: %s", err))
		return
	}
	if httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete object store, got non-200 response: %d", httpResp.StatusCode))
		return
	}
}

func (r *ObjectStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ObjectStoreResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Find object store
	client := *r.httpClient
	foundObjectStore, err := findObjectStore(client, state.Name.ValueString(), r.httpEndpoint, r.httpAuthToken)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to list object stores, got error: %s", err))
		return
	}
	if foundObjectStore == nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read object store, object store with name \"%s\" not found", state.Name.ValueString()))
		return
	}

	state.Id = types.StringValue(foundObjectStore.Name)
	state.S3BucketName = types.StringValue(foundObjectStore.StorageConfig.S3.BucketName)
	state.S3Prefix = types.StringValue(foundObjectStore.StorageConfig.S3.Prefix)
	state.S3IamRoleArn = types.StringValue(foundObjectStore.StorageConfig.S3.IamRoleArn)
	state.ValkeyClusterName = types.StringValue(foundObjectStore.CacheConfig.ValkeyCluster.ClusterName)
	if foundObjectStore.AccessLoggingConfig.Cloudwatch.LogGroupName != "" {
		state.AccessLoggingConfig = &AccessLoggingConfig{
			LogGroupName: types.StringValue(foundObjectStore.AccessLoggingConfig.Cloudwatch.LogGroupName),
			IamRoleArn:   types.StringValue(foundObjectStore.AccessLoggingConfig.Cloudwatch.IamRoleArn),
			Region:       types.StringValue(foundObjectStore.AccessLoggingConfig.Cloudwatch.Region),
		}
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *ObjectStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state ObjectStoreResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddError("Internal Error", "Object Store resource does not yet support updates, please delete and recreate the resource to make changes")
}

func (r *ObjectStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

type DescribeObjectStoresResponseData struct {
	Name          string `json:"name"`
	StorageConfig struct {
		S3 struct {
			BucketName string `json:"bucket_name"`
			Prefix     string `json:"prefix"`
			IamRoleArn string `json:"iam_role_arn"`
		} `json:"s3"`
	} `json:"storage_config"`
	CacheConfig struct {
		ValkeyCluster struct {
			ClusterName string `json:"cluster_name"`
		} `json:"valkey_cluster"`
	} `json:"cache_config"`
	AccessLoggingConfig struct {
		Cloudwatch struct {
			LogGroupName string `json:"log_group_name"`
			IamRoleArn   string `json:"iam_role_arn"`
			Region       string `json:"region"`
		} `json:"cloudwatch"`
	} `json:"access_logging_config"`
}

func findObjectStore(client http.Client, name string, httpEndpoint string, httpAuthToken string) (*DescribeObjectStoresResponseData, error) {
	getRequest, err := http.NewRequest("GET", fmt.Sprintf("%s/objectstore/%s", httpEndpoint, name), nil)
	if err != nil {
		return nil, err
	}
	getRequest.Header.Set("Authorization", httpAuthToken)
	getResp, err := client.Do(getRequest)
	if err != nil {
		return nil, err
	}
	if getResp.StatusCode >= 300 {
		body, _ := io.ReadAll(getResp.Body)
		err = getResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("unable to close HTTP response body, got error: %s", err)
		}
		return nil, fmt.Errorf("unable to list object store, got non-200 response: %s %s", getResp.Status, string(body))
	}

	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var objectStore DescribeObjectStoresResponseData
	err = json.Unmarshal(bodyBytes, &objectStore)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return &objectStore, nil
}
