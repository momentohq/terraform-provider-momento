package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource               = &ObjectStoreResource{}
	_ resource.ResourceWithConfigure  = &ObjectStoreResource{}
	_ resource.ResourceWithModifyPlan = &ObjectStoreResource{}
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

type MetricsConfig struct {
	Region     types.String `tfsdk:"region"`
	IamRoleArn types.String `tfsdk:"iam_role_arn"`
}

type ThrottlingLimitsConfig struct {
	ReadOperationsPerSecond  types.Int64 `tfsdk:"read_operations_per_second"`
	WriteOperationsPerSecond types.Int64 `tfsdk:"write_operations_per_second"`
	ReadBytesPerSecond       types.Int64 `tfsdk:"read_bytes_per_second"`
	WriteBytesPerSecond      types.Int64 `tfsdk:"write_bytes_per_second"`
}

var perRouterThrottlingLimitsAttrTypes = map[string]attr.Type{
	"read_operations_per_second":  types.Int64Type,
	"write_operations_per_second": types.Int64Type,
	"read_bytes_per_second":       types.Int64Type,
	"write_bytes_per_second":      types.Int64Type,
}

// ObjectStoreResourceModel describes the resource data model.
type ObjectStoreResourceModel struct {
	Id                        types.String            `tfsdk:"id"`
	Name                      types.String            `tfsdk:"name"`
	S3BucketName              types.String            `tfsdk:"s3_bucket_name"`
	S3Prefix                  types.String            `tfsdk:"s3_prefix"`
	S3IamRoleArn              types.String            `tfsdk:"s3_iam_role_arn"`
	ValkeyClusterName         types.String            `tfsdk:"valkey_cluster_name"`
	AccessLoggingConfig       *AccessLoggingConfig    `tfsdk:"access_logging_config"`
	MetricsConfig             *MetricsConfig          `tfsdk:"metrics_config"`
	ThrottlingLimits          *ThrottlingLimitsConfig `tfsdk:"throttling_limits"`
	PerRouterThrottlingLimits types.Object            `tfsdk:"per_router_throttling_limits"`
	RouterCount               types.Int64             `tfsdk:"router_count"`
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
					stringplanmodifier.RequiresReplace(),
				},
			},
			"s3_bucket_name": schema.StringAttribute{
				MarkdownDescription: "Name of the S3 bucket for the Object Store.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"s3_prefix": schema.StringAttribute{
				MarkdownDescription: "Optional prefix path within the S3 bucket.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
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
					stringplanmodifier.RequiresReplace(),
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
			"metrics_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Optional configuration for exporting CloudWatch metrics.",
				Optional:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"region": schema.StringAttribute{
						MarkdownDescription: "The AWS region where the metrics will be exported to.",
						Required:            true,
					},
					"iam_role_arn": schema.StringAttribute{
						MarkdownDescription: "The ARN of the IAM role that Momento will assume to export metrics.",
						Required:            true,
					},
				},
			},
			"throttling_limits": schema.SingleNestedAttribute{
				MarkdownDescription: "Optional configuration for request throttling limits.",
				Optional:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"read_operations_per_second": schema.Int64Attribute{
						MarkdownDescription: "The maximum number of read requests per second that Momento will accept for this object store across all routers. This is used to prevent overwhelming the Object Store with requests. If not set, Momento will use a default limit.",
						Optional:            true,
					},
					"write_operations_per_second": schema.Int64Attribute{
						MarkdownDescription: "The maximum number of write requests per second that Momento will accept for this object store across all routers. This is used to prevent overwhelming the Object Store with requests. If not set, Momento will use a default limit.",
						Optional:            true,
					},
					"read_bytes_per_second": schema.Int64Attribute{
						MarkdownDescription: "The maximum read throughput (bytes per second) that Momento will accept for this object store across all routers. This is used to prevent overwhelming the Object Store with requests. If not set, Momento will use a default limit.",
						Optional:            true,
					},
					"write_bytes_per_second": schema.Int64Attribute{
						MarkdownDescription: "The maximum write throughput (bytes per second) that Momento will accept for this object store across all routers. This is used to prevent overwhelming the Object Store with requests. If not set, Momento will use a default limit.",
						Optional:            true,
					},
				},
			},
			"per_router_throttling_limits": schema.SingleNestedAttribute{
				MarkdownDescription: "The per-router-node throttling limits (aggregate limits divided by router_count) sent to the Momento API.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"read_operations_per_second": schema.Int64Attribute{
						Computed: true,
					},
					"write_operations_per_second": schema.Int64Attribute{
						Computed: true,
					},
					"read_bytes_per_second": schema.Int64Attribute{
						Computed: true,
					},
					"write_bytes_per_second": schema.Int64Attribute{
						Computed: true,
					},
				},
			},
			"router_count": schema.Int64Attribute{
				MarkdownDescription: "The number of Momento router nodes backing this object store, computed from the /endpoints API.",
				Computed:            true,
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

type AttributeError struct {
	AttributePath path.Path
	Summary       string
	Detail        string
}

func validateObjectStorePlan(plan *ObjectStoreResourceModel) *AttributeError {
	if plan.Name.IsNull() || plan.Name.IsUnknown() || plan.Name.ValueString() == "" {
		return &AttributeError{
			AttributePath: path.Root("name"),
			Summary:       "Missing required value",
			Detail:        "The Object Store name is required.",
		}
	}
	if plan.S3BucketName.IsNull() || plan.S3BucketName.IsUnknown() || plan.S3BucketName.ValueString() == "" {
		return &AttributeError{
			AttributePath: path.Root("s3_bucket_name"),
			Summary:       "Missing required value",
			Detail:        "The S3 bucket name is required.",
		}
	}
	if plan.S3IamRoleArn.IsNull() || plan.S3IamRoleArn.IsUnknown() || plan.S3IamRoleArn.ValueString() == "" || len(plan.S3IamRoleArn.ValueString()) < 20 || !strings.HasPrefix(plan.S3IamRoleArn.ValueString(), "arn:aws:") {
		return &AttributeError{
			AttributePath: path.Root("s3_iam_role_arn"),
			Summary:       "Missing required value",
			Detail:        "The S3 IAM Role ARN is required.",
		}
	}
	if plan.ValkeyClusterName.IsNull() || plan.ValkeyClusterName.IsUnknown() || plan.ValkeyClusterName.ValueString() == "" {
		return &AttributeError{
			AttributePath: path.Root("valkey_cluster_name"),
			Summary:       "Missing required value",
			Detail:        "The Valkey Cluster name is required.",
		}
	}
	if plan.AccessLoggingConfig != nil {
		if plan.AccessLoggingConfig.LogGroupName.IsNull() || plan.AccessLoggingConfig.LogGroupName.IsUnknown() || plan.AccessLoggingConfig.LogGroupName.ValueString() == "" {
			return &AttributeError{
				AttributePath: path.Root("access_logging_config").AtName("log_group_name"),
				Summary:       "Missing required value",
				Detail:        "The CloudWatch Log Group name is required when access logging config is set.",
			}
		}
		if plan.AccessLoggingConfig.IamRoleArn.IsNull() || plan.AccessLoggingConfig.IamRoleArn.IsUnknown() || plan.AccessLoggingConfig.IamRoleArn.ValueString() == "" || len(plan.AccessLoggingConfig.IamRoleArn.ValueString()) < 20 || !strings.HasPrefix(plan.AccessLoggingConfig.IamRoleArn.ValueString(), "arn:aws:") {
			return &AttributeError{
				AttributePath: path.Root("access_logging_config").AtName("iam_role_arn"),
				Summary:       "Missing required value",
				Detail:        "The IAM Role ARN is required when access logging config is set.",
			}
		}
		if plan.AccessLoggingConfig.Region.IsNull() || plan.AccessLoggingConfig.Region.IsUnknown() || plan.AccessLoggingConfig.Region.ValueString() == "" {
			return &AttributeError{
				AttributePath: path.Root("access_logging_config").AtName("region"),
				Summary:       "Missing required value",
				Detail:        "The AWS region is required when access logging config is set.",
			}
		}
	}
	if plan.MetricsConfig != nil {
		if plan.MetricsConfig.IamRoleArn.IsNull() || plan.MetricsConfig.IamRoleArn.IsUnknown() || plan.MetricsConfig.IamRoleArn.ValueString() == "" || len(plan.MetricsConfig.IamRoleArn.ValueString()) < 20 || !strings.HasPrefix(plan.MetricsConfig.IamRoleArn.ValueString(), "arn:aws:") {
			return &AttributeError{
				AttributePath: path.Root("metrics_config").AtName("iam_role_arn"),
				Summary:       "Missing required value",
				Detail:        "The IAM Role ARN is required when metrics config is set.",
			}
		}
		if plan.MetricsConfig.Region.IsNull() || plan.MetricsConfig.Region.IsUnknown() || plan.MetricsConfig.Region.ValueString() == "" {
			return &AttributeError{
				AttributePath: path.Root("metrics_config").AtName("region"),
				Summary:       "Missing required value",
				Detail:        "The AWS region is required when metrics config is set.",
			}
		}
	}
	if plan.ThrottlingLimits != nil {
		if !plan.ThrottlingLimits.ReadOperationsPerSecond.IsNull() && plan.ThrottlingLimits.ReadOperationsPerSecond.ValueInt64() <= 0 {
			return &AttributeError{
				AttributePath: path.Root("throttling_limits").AtName("read_operations_per_second"),
				Summary:       "Invalid value",
				Detail:        "Read operations per second must be a positive integer.",
			}
		}
		if !plan.ThrottlingLimits.WriteOperationsPerSecond.IsNull() && plan.ThrottlingLimits.WriteOperationsPerSecond.ValueInt64() <= 0 {
			return &AttributeError{
				AttributePath: path.Root("throttling_limits").AtName("write_operations_per_second"),
				Summary:       "Invalid value",
				Detail:        "Write operations per second must be a positive integer.",
			}
		}
		if !plan.ThrottlingLimits.ReadBytesPerSecond.IsNull() && plan.ThrottlingLimits.ReadBytesPerSecond.ValueInt64() <= 0 {
			return &AttributeError{
				AttributePath: path.Root("throttling_limits").AtName("read_bytes_per_second"),
				Summary:       "Invalid value",
				Detail:        "Read bytes per second must be a positive integer.",
			}
		}
		if !plan.ThrottlingLimits.WriteBytesPerSecond.IsNull() && plan.ThrottlingLimits.WriteBytesPerSecond.ValueInt64() <= 0 {
			return &AttributeError{
				AttributePath: path.Root("throttling_limits").AtName("write_bytes_per_second"),
				Summary:       "Invalid value",
				Detail:        "Write bytes per second must be a positive integer.",
			}
		}
	}
	return nil
}

func computePerRouterLimits(ctx context.Context, aggregate *ThrottlingLimitsConfig, routerCount int64) (*ThrottlingLimitsConfig, types.Object, error) {
	if aggregate == nil || routerCount <= 0 {
		return nil, types.ObjectNull(perRouterThrottlingLimitsAttrTypes), nil
	}
	perRouter := ThrottlingLimitsConfig{
		ReadOperationsPerSecond:  types.Int64Null(),
		WriteOperationsPerSecond: types.Int64Null(),
		ReadBytesPerSecond:       types.Int64Null(),
		WriteBytesPerSecond:      types.Int64Null(),
	}
	if aggregate.ReadOperationsPerSecond.IsUnknown() {
		perRouter.ReadOperationsPerSecond = types.Int64Unknown()
	} else if !aggregate.ReadOperationsPerSecond.IsNull() {
		perRouter.ReadOperationsPerSecond = types.Int64Value(aggregate.ReadOperationsPerSecond.ValueInt64() / routerCount)
	}
	if aggregate.WriteOperationsPerSecond.IsUnknown() {
		perRouter.WriteOperationsPerSecond = types.Int64Unknown()
	} else if !aggregate.WriteOperationsPerSecond.IsNull() {
		perRouter.WriteOperationsPerSecond = types.Int64Value(aggregate.WriteOperationsPerSecond.ValueInt64() / routerCount)
	}
	if aggregate.ReadBytesPerSecond.IsUnknown() {
		perRouter.ReadBytesPerSecond = types.Int64Unknown()
	} else if !aggregate.ReadBytesPerSecond.IsNull() {
		perRouter.ReadBytesPerSecond = types.Int64Value(aggregate.ReadBytesPerSecond.ValueInt64() / routerCount)
	}
	if aggregate.WriteBytesPerSecond.IsUnknown() {
		perRouter.WriteBytesPerSecond = types.Int64Unknown()
	} else if !aggregate.WriteBytesPerSecond.IsNull() {
		perRouter.WriteBytesPerSecond = types.Int64Value(aggregate.WriteBytesPerSecond.ValueInt64() / routerCount)
	}
	obj, diags := types.ObjectValueFrom(ctx, perRouterThrottlingLimitsAttrTypes, perRouter)
	if diags.HasError() {
		return nil, types.ObjectNull(perRouterThrottlingLimitsAttrTypes), fmt.Errorf("error building per-router throttling limits object: %s", diags)
	}
	return &perRouter, obj, nil
}

func marshalObjectStoreRequest(plan *ObjectStoreResourceModel, perRouterLimits *ThrottlingLimitsConfig) (*bytes.Buffer, error) {
	requestData := ObjectStoreData{
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
		requestData.AccessLoggingConfig = &ObjectStoreAccessLoggingConfig{
			Cloudwatch: &ObjectStoreCloudwatchAccessLoggingConfig{
				LogGroupName: plan.AccessLoggingConfig.LogGroupName.ValueString(),
				IamRoleArn:   plan.AccessLoggingConfig.IamRoleArn.ValueString(),
				Region:       plan.AccessLoggingConfig.Region.ValueString(),
			},
		}
	}
	if plan.MetricsConfig != nil {
		requestData.MetricsConfig = &ObjectStoreMetricsConfig{
			Cloudwatch: &ObjectStoreCloudwatchMetricsConfig{
				IamRoleArn: plan.MetricsConfig.IamRoleArn.ValueString(),
				Region:     plan.MetricsConfig.Region.ValueString(),
			},
		}
	}
	if perRouterLimits != nil {
		limits := &ObjectStoreThrottlingLimits{}
		if !perRouterLimits.ReadOperationsPerSecond.IsNull() {
			v := perRouterLimits.ReadOperationsPerSecond.ValueInt64()
			limits.ReadOperationsPerSecond = &v
		}
		if !perRouterLimits.WriteOperationsPerSecond.IsNull() {
			v := perRouterLimits.WriteOperationsPerSecond.ValueInt64()
			limits.WriteOperationsPerSecond = &v
		}
		if !perRouterLimits.ReadBytesPerSecond.IsNull() {
			v := perRouterLimits.ReadBytesPerSecond.ValueInt64()
			limits.ReadBytesPerSecond = &v
		}
		if !perRouterLimits.WriteBytesPerSecond.IsNull() {
			v := perRouterLimits.WriteBytesPerSecond.ValueInt64()
			limits.WriteBytesPerSecond = &v
		}
		if limits.ReadOperationsPerSecond != nil || limits.WriteOperationsPerSecond != nil ||
			limits.ReadBytesPerSecond != nil || limits.WriteBytesPerSecond != nil {
			requestData.ThrottlingLimits = limits
		}
	}
	requestJson, err := json.Marshal(requestData)
	if err != nil {
		return nil, err
	}
	requestBody := bytes.NewBuffer(requestJson)
	return requestBody, nil
}

func sendObjectStoreRequest(plan *ObjectStoreResourceModel, r *ObjectStoreResource, requestBody *bytes.Buffer) error {
	client := *r.httpClient
	putUrl := fmt.Sprintf("%s/objectstore/%s", r.httpEndpoint, plan.Name.ValueString())
	putRequest, err := http.NewRequest("PUT", putUrl, requestBody)
	if err != nil {
		return err
	}
	putRequest.Header.Set("Content-Type", "application/json")
	putRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(putRequest)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("unable to create or update object store, got non-200 response: %s %s", httpResp.Status, string(body))
	}
	return nil
}

// Will detect if router count has changed and produce a diff so that next terraform apply
// will update the object store with new per-router throttling limits.
func (r *ObjectStoreResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Only run during updates: skip Create (state null), Delete (plan null), or unconfigured provider.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || r.httpClient == nil {
		return
	}

	var state ObjectStoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var plan ObjectStoreResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only relevant when throttling limits are configured.
	if plan.ThrottlingLimits == nil {
		return
	}

	routerCount, err := fetchRouterCount(*r.httpClient, r.httpEndpoint, r.httpAuthToken)
	if err != nil {
		// Don't fail the plan on a transient error fetching router count, but surface a warning.
		resp.Diagnostics.AddWarning(
			"Unable to fetch router count",
			fmt.Sprintf("Failed to fetch router count from %q: %v.", r.httpEndpoint, err),
		)
		return
	}

	if !state.RouterCount.IsNull() && !state.RouterCount.IsUnknown() && routerCount == state.RouterCount.ValueInt64() {
		return
	}

	plan.RouterCount = types.Int64Value(routerCount)
	_, perRouterLimitsObj, err := computePerRouterLimits(ctx, plan.ThrottlingLimits, routerCount)
	if err != nil {
		resp.Diagnostics.AddError("ModifyPlan Error", fmt.Sprintf("Unable to compute per-router throttling limits: %s", err))
		return
	}
	plan.PerRouterThrottlingLimits = perRouterLimitsObj
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *ObjectStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ObjectStoreResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	validationErr := validateObjectStorePlan(&plan)
	if validationErr != nil {
		resp.Diagnostics.AddAttributeError(validationErr.AttributePath, validationErr.Summary, validationErr.Detail)
		return
	}

	routerCount, err := fetchRouterCount(*r.httpClient, r.httpEndpoint, r.httpAuthToken)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to fetch router node count: %s", err))
		return
	}
	plan.RouterCount = types.Int64Value(routerCount)
	perRouterLimits, perRouterLimitsObj, err := computePerRouterLimits(ctx, plan.ThrottlingLimits, routerCount)
	if err != nil {
		resp.Diagnostics.AddError("Create Error", err.Error())
		return
	}
	plan.PerRouterThrottlingLimits = perRouterLimitsObj

	// Create and allow retrying up to 3 times in case of eventual consistency issues
	// with the Valkey Cluster or IAM roles coming online.
	if err = r.applyObjectStoreWithRetry(ctx, &plan, perRouterLimits); err != nil {
		resp.Diagnostics.AddError("Unable to Create Object Store",
			fmt.Sprintf("An unexpected error occurred when creating the object store. "+
				"If the error is not clear, please contact the provider developers.\n\nError: %s", err))
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
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
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
	foundObjectStore, err := describeObjectStore(client, state.Name.ValueString(), r.httpEndpoint, r.httpAuthToken)
	if foundObjectStore == nil && err == nil {
		// Object store not found, remove from state
		resp.Diagnostics.AddWarning("Object Store Not Found", fmt.Sprintf("The object store with name \"%s\" was not found. It may have been deleted outside of Terraform. Removing from state.", state.Name.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read object store, got error: %s", err))
		return
	}

	state.Id = types.StringValue(foundObjectStore.Name)
	state.S3BucketName = types.StringValue(foundObjectStore.StorageConfig.S3.BucketName)
	if foundObjectStore.StorageConfig.S3.Prefix != "" {
		state.S3Prefix = types.StringValue(foundObjectStore.StorageConfig.S3.Prefix)
	}
	state.S3IamRoleArn = types.StringValue(foundObjectStore.StorageConfig.S3.IamRoleArn)
	state.ValkeyClusterName = types.StringValue(foundObjectStore.CacheConfig.ValkeyCluster.ClusterName)
	if foundObjectStore.AccessLoggingConfig != nil && foundObjectStore.AccessLoggingConfig.Cloudwatch != nil {
		state.AccessLoggingConfig = &AccessLoggingConfig{
			LogGroupName: types.StringValue(foundObjectStore.AccessLoggingConfig.Cloudwatch.LogGroupName),
			IamRoleArn:   types.StringValue(foundObjectStore.AccessLoggingConfig.Cloudwatch.IamRoleArn),
			Region:       types.StringValue(foundObjectStore.AccessLoggingConfig.Cloudwatch.Region),
		}
	}
	if foundObjectStore.MetricsConfig != nil && foundObjectStore.MetricsConfig.Cloudwatch != nil {
		state.MetricsConfig = &MetricsConfig{
			IamRoleArn: types.StringValue(foundObjectStore.MetricsConfig.Cloudwatch.IamRoleArn),
			Region:     types.StringValue(foundObjectStore.MetricsConfig.Cloudwatch.Region),
		}
	}
	// router_count and per_router_throttling_limits are intentionally not updated here.
	// They reflect the last-applied values so that ModifyPlan can detect router count
	// changes and trigger an Update to push corrected per-router limits to the API.

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *ObjectStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ObjectStoreResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	validationErr := validateObjectStorePlan(&plan)
	if validationErr != nil {
		resp.Diagnostics.AddAttributeError(validationErr.AttributePath, validationErr.Summary, validationErr.Detail)
		return
	}

	routerCount, err := fetchRouterCount(*r.httpClient, r.httpEndpoint, r.httpAuthToken)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to fetch router node count: %s", err))
		return
	}
	plan.RouterCount = types.Int64Value(routerCount)
	perRouterLimits, perRouterLimitsObj, err := computePerRouterLimits(ctx, plan.ThrottlingLimits, routerCount)
	if err != nil {
		resp.Diagnostics.AddError("Update Error", err.Error())
		return
	}
	plan.PerRouterThrottlingLimits = perRouterLimitsObj

	// Update and allow retrying up to 3 times in case of transient errors.
	if err = r.applyObjectStoreWithRetry(ctx, &plan, perRouterLimits); err != nil {
		resp.Diagnostics.AddError("Unable to Update Object Store",
			fmt.Sprintf("An unexpected error occurred when updating the object store. "+
				"If the error is not clear, please contact the provider developers.\n\nError: %s", err))
		return
	}

	// Map response body to schema and populate computed attribute values
	plan.Id = types.StringValue(plan.Name.ValueString())

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ObjectStoreResource) applyObjectStoreWithRetry(ctx context.Context, plan *ObjectStoreResourceModel, perRouterLimits *ThrottlingLimitsConfig) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	attempt := 0
	var lastErr error
	for attempt < 4 {
		requestBody, err := marshalObjectStoreRequest(plan, perRouterLimits)
		if err != nil {
			return fmt.Errorf("unable to marshal object store request to JSON: %w", err)
		}
		if err = sendObjectStoreRequest(plan, r, requestBody); err != nil {
			lastErr = err
		} else {
			return nil
		}
		attempt++
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting to retry: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	return lastErr
}

func (r *ObjectStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

type ObjectStoreCloudwatchMetricsConfig struct {
	IamRoleArn string `json:"iam_role_arn"`
	Region     string `json:"region"`
}

type ObjectStoreMetricsConfig struct {
	Cloudwatch *ObjectStoreCloudwatchMetricsConfig `json:"cloudwatch,omitempty"`
}

type ObjectStoreCloudwatchAccessLoggingConfig struct {
	LogGroupName string `json:"log_group_name"`
	IamRoleArn   string `json:"iam_role_arn"`
	Region       string `json:"region"`
}

type ObjectStoreAccessLoggingConfig struct {
	Cloudwatch *ObjectStoreCloudwatchAccessLoggingConfig `json:"cloudwatch,omitempty"`
}

type ObjectStoreThrottlingLimits struct {
	ReadOperationsPerSecond  *int64 `json:"read_operations_per_second,omitempty"`
	WriteOperationsPerSecond *int64 `json:"write_operations_per_second,omitempty"`
	ReadBytesPerSecond       *int64 `json:"read_bytes_per_second,omitempty"`
	WriteBytesPerSecond      *int64 `json:"write_bytes_per_second,omitempty"`
}

type ObjectStoreData struct {
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
	AccessLoggingConfig *ObjectStoreAccessLoggingConfig `json:"access_logging_config,omitempty"`
	MetricsConfig       *ObjectStoreMetricsConfig       `json:"metrics_config,omitempty"`
	ThrottlingLimits    *ObjectStoreThrottlingLimits    `json:"object_store_limits,omitempty"`
}

func fetchRouterCount(client http.Client, httpEndpoint string, httpAuthToken string) (int64, error) {
	getRequest, err := http.NewRequest("GET", fmt.Sprintf("%s/endpoints", httpEndpoint), nil)
	if err != nil {
		return 0, err
	}
	getRequest.Header.Set("Authorization", httpAuthToken)
	getResp, err := client.Do(getRequest)
	if err != nil {
		return 0, err
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode >= 300 {
		body, _ := io.ReadAll(getResp.Body)
		return 0, fmt.Errorf("unable to fetch endpoints, got non-2xx response: %s %s", getResp.Status, string(body))
	}
	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading endpoints response body: %v", err)
	}
	// Response is a map of AZ name -> list of socket addresses
	var endpoints map[string][]struct {
		SocketAddress string `json:"socket_address"`
	}
	if err = json.Unmarshal(bodyBytes, &endpoints); err != nil {
		return 0, fmt.Errorf("error unmarshalling endpoints response: %v", err)
	}
	var count int64
	for _, nodes := range endpoints {
		count += int64(len(nodes))
	}
	return count, nil
}

func describeObjectStore(client http.Client, name string, httpEndpoint string, httpAuthToken string) (*ObjectStoreData, error) {
	getRequest, err := http.NewRequest("GET", fmt.Sprintf("%s/objectstore/%s", httpEndpoint, name), nil)
	if err != nil {
		return nil, err
	}
	getRequest.Header.Set("Authorization", httpAuthToken)
	getResp, err := client.Do(getRequest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode == 404 {
		return nil, nil
	}
	if getResp.StatusCode >= 300 {
		body, _ := io.ReadAll(getResp.Body)
		return nil, fmt.Errorf("unable to describe object store, got non-2xx response: %s %s", getResp.Status, string(body))
	}

	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var objectStore ObjectStoreData
	err = json.Unmarshal(bodyBytes, &objectStore)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return &objectStore, nil
}
