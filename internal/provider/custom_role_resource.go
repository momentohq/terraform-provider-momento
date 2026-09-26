package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &CustomRoleResource{}
	_ resource.ResourceWithConfigure   = &CustomRoleResource{}
	_ resource.ResourceWithImportState = &CustomRoleResource{}
	_ resource.ResourceWithIdentity    = &CustomRoleResource{}
)

func NewCustomRoleResource() resource.Resource {
	return &CustomRoleResource{}
}

// CustomRoleResource defines the resource implementation.
type CustomRoleResource struct {
	httpClient      *http.Client
	httpMgaEndpoint string
	httpAuthToken   string
}

type PermissionAction string

const (
	PermissionActionRead   PermissionAction = "read"
	PermissionActionWrite  PermissionAction = "write"
	PermissionActionList   PermissionAction = "list"
	PermissionActionInvoke PermissionAction = "invoke"
)

type AllSelectorModel struct {
	All types.Bool `tfsdk:"all"`
}

type NameSelectorModel struct {
	All  types.Bool   `tfsdk:"all"`
	Name types.String `tfsdk:"name"`
}

type NameOrPrefixSelectorModel struct {
	All    types.Bool   `tfsdk:"all"`
	Name   types.String `tfsdk:"name"`
	Prefix types.String `tfsdk:"prefix"`
}

type ItemSelectorModel struct {
	All       types.Bool   `tfsdk:"all"`
	KeyName   types.String `tfsdk:"key"`
	KeyPrefix types.String `tfsdk:"key_prefix"`
}

type RuleType string

const (
	RuleTypeCache              RuleType = "cache"
	RuleTypeTopic              RuleType = "topic"
	RuleTypeStore              RuleType = "store"
	RuleTypeFunction           RuleType = "function"
	RuleTypeDatabase           RuleType = "database"
	RuleTypeAccountManagement  RuleType = "account_management"
	RuleTypeAuthManagement     RuleType = "auth_management"
	RuleTypeResourceManagement RuleType = "resource_management"
)

type RuleModel struct {
	Type        RuleType                   `tfsdk:"type"`
	Permissions []PermissionAction         `tfsdk:"permissions"`
	Caches      *NameSelectorModel         `tfsdk:"caches"`
	Items       *ItemSelectorModel         `tfsdk:"items"`
	Topics      *NameOrPrefixSelectorModel `tfsdk:"topics"`
	Stores      *NameSelectorModel         `tfsdk:"stores"`
	Functions   *NameOrPrefixSelectorModel `tfsdk:"functions"`
	Databases   *NameSelectorModel         `tfsdk:"databases"`
	Resources   *AllSelectorModel          `tfsdk:"resources"`
}

type ConditionModel struct {
	IpFilter struct {
		AllowedCidrRanges []types.String `tfsdk:"allowed_cidr_ranges"`
	} `tfsdk:"ip_filter"`
}

type PermissionsModel struct {
	Rules      []RuleModel      `tfsdk:"rules"`
	Conditions []ConditionModel `tfsdk:"conditions"`
}

// CustomRoleResourceModel describes the resource data model.
type CustomRoleResourceModel struct {
	Id          types.String      `tfsdk:"id"`
	Name        types.String      `tfsdk:"name"`
	Description types.String      `tfsdk:"description"`
	Permissions *PermissionsModel `tfsdk:"permissions"`
}

// CustomRoleIdentityModel describes the resource identity data model.
type CustomRoleIdentityModel struct {
	Name types.String `tfsdk:"name"`
}

func (r *CustomRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_role"
	// The identity is the role name, which can be changed in place.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *CustomRoleResource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"name": identityschema.StringAttribute{
				Description:       "The name of the Custom Role.",
				RequiredForImport: true,
			},
		},
	}
}

func nameSelectorAttribute(resources string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: fmt.Sprintf("Which %s this rule applies to. Exactly one of `all` or `name` must be set.", resources),
		Optional:            true,
		Attributes: map[string]schema.Attribute{
			"all": schema.BoolAttribute{
				Optional: true,
				Validators: []validator.Bool{
					boolvalidator.Equals(true),
					// On a child rather than the selector itself, so e.g. cache rules accept a null function selector.
					boolvalidator.ExactlyOneOf(
						path.MatchRelative().AtParent().AtName("name"),
					),
				},
			},
			"name": schema.StringAttribute{Optional: true},
		},
	}
}

func nameOrPrefixSelectorAttribute(resources string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: fmt.Sprintf("Which %s this rule applies to. Exactly one of `all`, `name`, or `prefix` must be set.", resources),
		Optional:            true,
		Attributes: map[string]schema.Attribute{
			"all": schema.BoolAttribute{
				Optional: true,
				Validators: []validator.Bool{
					boolvalidator.Equals(true),
					// On a child rather than the selector itself, so e.g. cache rules accept a null function selector.
					boolvalidator.ExactlyOneOf(
						path.MatchRelative().AtParent().AtName("name"),
						path.MatchRelative().AtParent().AtName("prefix"),
					),
				},
			},
			"name":   schema.StringAttribute{Optional: true},
			"prefix": schema.StringAttribute{Optional: true},
		},
	}
}

// Available resource selectors, depending on the rule type.
var ruleSelectorAttributes = []string{"caches", "items", "topics", "stores", "functions", "databases", "resources"}

type ruleTypeValidator struct {
	ruleType          RuleType
	requiredSelectors []string
	// mustBeAll means the required selectors only accept `all = true`.
	mustBeAll bool
}

// ruleTypeRequires requires exactly the given selectors for rules of the given type.
func ruleTypeRequires(ruleType RuleType, required ...string) validator.String {
	return ruleTypeValidator{ruleType: ruleType, requiredSelectors: required}
}

// ruleTypeRequiresAll is like ruleTypeRequires, but the selectors must be `all`.
func ruleTypeRequiresAll(ruleType RuleType, requiredSelectors ...string) validator.String {
	return ruleTypeValidator{ruleType: ruleType, requiredSelectors: requiredSelectors, mustBeAll: true}
}

func (v ruleTypeValidator) Description(ctx context.Context) string {
	if len(v.requiredSelectors) == 0 {
		return fmt.Sprintf("rules of type %s cannot set any selectors", v.ruleType)
	}
	return fmt.Sprintf("rules of type %s must set exactly these selectors: %v", v.ruleType, v.requiredSelectors)
}

func (v ruleTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ruleTypeValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.ConfigValue.ValueString() != string(v.ruleType) {
		return
	}

	rulePath := req.Path.ParentPath()
	for _, name := range ruleSelectorAttributes {
		selectorPath := rulePath.AtName(name)

		var selector types.Object
		diags := req.Config.GetAttribute(ctx, selectorPath, &selector)
		resp.Diagnostics.Append(diags...)
		if diags.HasError() || selector.IsUnknown() {
			continue
		}

		isRequired := false
		for _, requiredName := range v.requiredSelectors {
			if requiredName == name {
				isRequired = true
				break
			}
		}

		if !isRequired {
			if !selector.IsNull() {
				resp.Diagnostics.AddAttributeError(selectorPath, "Invalid value",
					fmt.Sprintf("`%s` cannot be set for rule type %s", name, v.ruleType))
			}
			continue
		}

		if selector.IsNull() {
			resp.Diagnostics.AddAttributeError(selectorPath, "Missing value",
				fmt.Sprintf("`%s` is required for rule type %s", name, v.ruleType))
			continue
		}

		if v.mustBeAll {
			all, ok := selector.Attributes()["all"].(types.Bool)
			if ok && all.IsUnknown() {
				continue
			}
			if !ok || !all.ValueBool() {
				resp.Diagnostics.AddAttributeError(selectorPath, "Invalid value",
					fmt.Sprintf("`%s` must be `all` for rule type %s", name, v.ruleType))
			}
		}
	}
}

func (r *CustomRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Custom Role.",

		Attributes: map[string]schema.Attribute{
			// The testing framework requires an id attribute to be present in every data source and resource
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the Custom Role.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the Custom Role.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The description of the Custom Role.",
				Optional:            true,
				// Plan & send a removed description as ""; null would keep the original description.
				Computed: true,
				Default:  stringdefault.StaticString(""),
			},
			"permissions": schema.SingleNestedAttribute{
				MarkdownDescription: "The permission grant of the Custom Role.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"rules": schema.ListNestedAttribute{
						MarkdownDescription: "The permission rules that make up the Custom Role.",
						Required:            true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"type": schema.StringAttribute{
									Description: "The rule type.",
									Required:    true,
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(RuleTypeCache),
											string(RuleTypeTopic),
											string(RuleTypeStore),
											string(RuleTypeFunction),
											string(RuleTypeDatabase),
											string(RuleTypeAccountManagement),
											string(RuleTypeAuthManagement),
											string(RuleTypeResourceManagement),
										),
										ruleTypeRequires(RuleTypeCache, "caches", "items"),
										ruleTypeRequires(RuleTypeTopic, "caches", "topics"),
										ruleTypeRequires(RuleTypeStore, "stores", "items"),
										ruleTypeRequires(RuleTypeFunction, "caches", "functions"),
										ruleTypeRequires(RuleTypeDatabase, "databases", "items"),
										ruleTypeRequires(RuleTypeAccountManagement),
										ruleTypeRequiresAll(RuleTypeAuthManagement, "items"),
										ruleTypeRequiresAll(RuleTypeResourceManagement, "resources"),
									},
								},
								"permissions": schema.ListAttribute{
									MarkdownDescription: "The actions this rule allows.",
									Required:            true,
									ElementType:         types.StringType,
									Validators: []validator.List{
										listvalidator.ValueStringsAre(
											stringvalidator.OneOf(
												string(PermissionActionRead),
												string(PermissionActionWrite),
												string(PermissionActionList),
												string(PermissionActionInvoke),
											),
										),
									},
								},
								"items": schema.SingleNestedAttribute{
									MarkdownDescription: "Which items this rule applies to. Exactly one of `all`, `key`, or `key_prefix` must be set.",
									Optional:            true,
									Attributes: map[string]schema.Attribute{
										"all": schema.BoolAttribute{
											Optional: true,
											Validators: []validator.Bool{
												boolvalidator.Equals(true),
												// On a child rather than the selector itself, so e.g. cache rules accept a null function selector.
												boolvalidator.ExactlyOneOf(
													path.MatchRelative().AtParent().AtName("key"),
													path.MatchRelative().AtParent().AtName("key_prefix"),
												),
											},
										},
										"key":        schema.StringAttribute{Optional: true},
										"key_prefix": schema.StringAttribute{Optional: true},
									},
								},
								"caches":    nameSelectorAttribute("caches"),
								"topics":    nameOrPrefixSelectorAttribute("topics"),
								"stores":    nameSelectorAttribute("stores"),
								"functions": nameOrPrefixSelectorAttribute("functions"),
								"databases": nameSelectorAttribute("databases"),
								"resources": schema.SingleNestedAttribute{
									MarkdownDescription: "Which resources this rule applies to. Must be `all`.",
									Optional:            true,
									Attributes: map[string]schema.Attribute{
										"all": schema.BoolAttribute{Required: true, Validators: []validator.Bool{boolvalidator.Equals(true)}},
									},
								},
							},
						},
					},
					"conditions": schema.ListNestedAttribute{
						MarkdownDescription: "Additional constraints that apply to the whole permission set.",
						Optional:            true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"ip_filter": schema.SingleNestedAttribute{
									Required: true, // required while it's the only option
									Attributes: map[string]schema.Attribute{
										"allowed_cidr_ranges": schema.ListAttribute{
											MarkdownDescription: "The CIDR ranges from which requests are allowed.",
											Required:            true,
											ElementType:         types.StringType,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *CustomRoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
	r.httpMgaEndpoint = clients.httpMgaEndpoint
	r.httpAuthToken = clients.httpAuthToken
}

type RuleData struct {
	Type        RuleType           `json:"type"`
	Permissions []PermissionAction `json:"permissions"`
	Caches      any                `json:"caches,omitempty"`
	Items       any                `json:"items,omitempty"`
	Topics      any                `json:"topics,omitempty"`
	Stores      any                `json:"stores,omitempty"`
	Functions   any                `json:"functions,omitempty"`
	Databases   any                `json:"databases,omitempty"`
	Resources   any                `json:"resources,omitempty"`
}

func allSelectorData(m *AllSelectorModel) any {
	if m == nil {
		return nil
	}
	return "*"
}

func nameSelectorData(m *NameSelectorModel) any {
	if m == nil {
		return nil
	}
	if m.All.ValueBool() == true {
		return "*"
	}
	return map[string]any{"name": m.Name.ValueString()}
}

func nameOrPrefixSelectorData(m *NameOrPrefixSelectorModel) any {
	if m == nil {
		return nil
	}
	if m.All.ValueBool() == true {
		return "*"
	} else if !m.Name.IsNull() && !m.Name.IsUnknown() {
		return map[string]any{"name": m.Name.ValueString()}
	} else if !m.Prefix.IsNull() && !m.Prefix.IsUnknown() {
		return map[string]any{"prefix": m.Prefix.ValueString()}
	}
	return nil
}

func itemSelectorData(m *ItemSelectorModel) any {
	if m == nil {
		return nil
	}
	if m.All.ValueBool() == true {
		return "*"
	} else if !m.KeyName.IsNull() && !m.KeyName.IsUnknown() {
		return map[string]any{"key": m.KeyName.ValueString()}
	} else if !m.KeyPrefix.IsNull() && !m.KeyPrefix.IsUnknown() {
		return map[string]any{"key_prefix": m.KeyPrefix.ValueString()}
	}
	return nil
}

func ruleData(rule RuleModel) RuleData {
	return RuleData{
		Type:        rule.Type,
		Permissions: rule.Permissions,
		Caches:      nameSelectorData(rule.Caches),
		Items:       itemSelectorData(rule.Items),
		Topics:      nameOrPrefixSelectorData(rule.Topics),
		Stores:      nameSelectorData(rule.Stores),
		Functions:   nameOrPrefixSelectorData(rule.Functions),
		Databases:   nameSelectorData(rule.Databases),
		Resources:   allSelectorData(rule.Resources),
	}
}

type IpFilterData struct {
	AllowedCidrRanges []string `json:"allowed_cidr_ranges"`
}

type ConditionData struct {
	IpFilter IpFilterData `json:"ip_filter"`
}

type PermissionsData struct {
	Rules      []RuleData      `json:"rules,omitzero"`
	Conditions []ConditionData `json:"conditions,omitzero"`
}

type CustomRoleData struct {
	Id          string          `json:"role_id"`
	Name        string          `json:"role_name"`
	Description string          `json:"description"`
	Permissions PermissionsData `json:"permissions"`
}

func (r *CustomRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CustomRoleResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	postUrl := fmt.Sprintf("%s/roles", r.httpMgaEndpoint)

	requestMap := CustomRoleData{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	}
	if plan.Permissions.Rules != nil {
		requestMap.Permissions.Rules = make([]RuleData, 0, len(plan.Permissions.Rules))
		for _, rule := range plan.Permissions.Rules {
			requestMap.Permissions.Rules = append(requestMap.Permissions.Rules, ruleData(rule))
		}
	}
	if plan.Permissions.Conditions != nil {
		requestMap.Permissions.Conditions = make([]ConditionData, 0, len(plan.Permissions.Conditions))
		for _, condition := range plan.Permissions.Conditions {
			allowedCidrRanges := make([]string, len(condition.IpFilter.AllowedCidrRanges))
			for i, cidr := range condition.IpFilter.AllowedCidrRanges {
				allowedCidrRanges[i] = cidr.ValueString()
			}
			requestMap.Permissions.Conditions = append(requestMap.Permissions.Conditions,
				ConditionData{
					IpFilter: IpFilterData{
						AllowedCidrRanges: allowedCidrRanges,
					},
				},
			)
		}
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal request body, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)
	postRequest, err := http.NewRequest("POST", postUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to create custom role, got error: %s", err))
		return
	}
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(postRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create custom role, got error: %s", err))
		return
	}
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create custom role, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}

	// Map response body to schema and populate computed attribute values
	var customRole CustomRoleData
	err = json.Unmarshal(body, &customRole)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to unmarshal JSON response from creating custom role, got error: %v", err))
		return
	}
	plan.Id = types.StringValue(customRole.Id)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, CustomRoleIdentityModel{Name: plan.Name})...)
}

type DeleteStatus string

const (
	DeleteStatusDeleted DeleteStatus = "deleted"
	DeleteStatusBlocked DeleteStatus = "blocked"
)

type AccountMemberData struct {
	UserName string `json:"user_name"`
}

type InvitationData struct {
	AccountMember AccountMemberData `json:"account_member"`
}

type ApiKeyData struct {
	KeyId                string `json:"key_id"`
	AccountId            string `json:"account_id"`
	Description          string `json:"description"`
	IssuedAtEpochSeconds int64  `json:"issued_at_epoch_seconds"`
}

type DeleteCustomRoleData struct {
	Status         DeleteStatus        `json:"status"`
	AccountMembers []AccountMemberData `json:"account_members"`
	Invitations    []InvitationData    `json:"invitations"`
	ApiKeys        []ApiKeyData        `json:"api_keys"`
}

func (d DeleteCustomRoleData) formatActiveReferences() string {
	var sections []string
	if len(d.AccountMembers) > 0 {
		lines := []string{"Account Members:"}
		for _, member := range d.AccountMembers {
			lines = append(lines, "- "+member.UserName)
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(d.Invitations) > 0 {
		lines := []string{"Invited Account Members:"}
		for _, invite := range d.Invitations {
			lines = append(lines, "- "+invite.AccountMember.UserName)
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(d.ApiKeys) > 0 {
		lines := []string{"API Keys:"}
		for _, key := range d.ApiKeys {
			issuedAt := time.Unix(key.IssuedAtEpochSeconds, 0).Format("2006-01-02 15:04:05 MST")
			lines = append(lines,
				"- Key ID: "+key.KeyId,
				"  Account ID: "+key.AccountId,
				"  Description: "+key.Description,
				"  Issued At: "+issuedAt,
			)
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return strings.Join(sections, "\n")
}

func (r *CustomRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CustomRoleResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	deleteUrl := fmt.Sprintf("%s/roles/%s", r.httpMgaEndpoint, state.Id.ValueString())
	deleteRequest, err := http.NewRequest("DELETE", deleteUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to delete custom role, got error: %s", err))
		return
	}
	deleteRequest.Header.Set("Authorization", r.httpAuthToken)

	httpResp, err := client.Do(deleteRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete custom role, got error: %s", err))
		return
	}
	if httpResp.StatusCode == 404 {
		// Already deleted
		return
	}
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete custom role, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}

	var deleteResponse DeleteCustomRoleData
	err = json.Unmarshal(body, &deleteResponse)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to unmarshal JSON response from deleting custom role, got error: %v", err))
		return
	}
	if deleteResponse.Status == DeleteStatusBlocked {
		resp.Diagnostics.AddError("Custom Role Error", fmt.Sprintf(
			"Unable to delete custom role, still in use:\n\n%s", deleteResponse.formatActiveReferences(),
		))
		return
	}
}

func allSelectorModel(v any) (*AllSelectorModel, error) {
	switch v {
	case nil:
		return nil, nil
	case "*":
		return &AllSelectorModel{All: types.BoolValue(true)}, nil
	}
	return nil, fmt.Errorf("unexpected selector %v", v)
}

func selectorFields(v any, keys ...string) (isNull bool, all types.Bool, fields map[string]types.String, err error) {
	fields = make(map[string]types.String, len(keys))
	for _, key := range keys {
		fields[key] = types.StringNull()
	}
	switch v := v.(type) {
	case nil:
		return true, types.BoolNull(), fields, nil
	case string:
		if v == "*" {
			return false, types.BoolValue(true), fields, nil
		}
	case map[string]any:
		for _, key := range keys {
			if value, ok := v[key].(string); ok {
				fields[key] = types.StringValue(value)
				return false, types.BoolNull(), fields, nil
			}
		}
	}
	return false, types.BoolNull(), fields, fmt.Errorf("unexpected selector %v", v)
}

func nameSelectorModel(v any) (*NameSelectorModel, error) {
	isNull, all, fields, err := selectorFields(v, "name")
	if isNull || err != nil {
		return nil, err
	}
	return &NameSelectorModel{All: all, Name: fields["name"]}, nil
}

func nameOrPrefixSelectorModel(v any) (*NameOrPrefixSelectorModel, error) {
	isNull, all, fields, err := selectorFields(v, "name", "prefix")
	if isNull || err != nil {
		return nil, err
	}
	return &NameOrPrefixSelectorModel{All: all, Name: fields["name"], Prefix: fields["prefix"]}, nil
}

func itemSelectorModel(v any) (*ItemSelectorModel, error) {
	isNull, all, fields, err := selectorFields(v, "key", "key_prefix")
	if isNull || err != nil {
		return nil, err
	}
	return &ItemSelectorModel{All: all, KeyName: fields["key"], KeyPrefix: fields["key_prefix"]}, nil
}

func ruleModel(rule RuleData) (RuleModel, error) {
	var errs []error
	collect := func(err error) { errs = append(errs, err) }
	model := RuleModel{Type: rule.Type, Permissions: rule.Permissions}
	var err error
	model.Caches, err = nameSelectorModel(rule.Caches)
	collect(err)
	model.Items, err = itemSelectorModel(rule.Items)
	collect(err)
	model.Topics, err = nameOrPrefixSelectorModel(rule.Topics)
	collect(err)
	model.Stores, err = nameSelectorModel(rule.Stores)
	collect(err)
	model.Functions, err = nameOrPrefixSelectorModel(rule.Functions)
	collect(err)
	model.Databases, err = nameSelectorModel(rule.Databases)
	collect(err)
	model.Resources, err = allSelectorModel(rule.Resources)
	collect(err)
	return model, errors.Join(errs...)
}

func permissionsModel(permissions PermissionsData) (*PermissionsModel, error) {
	model := &PermissionsModel{Rules: make([]RuleModel, 0, len(permissions.Rules))}
	for i, rule := range permissions.Rules {
		ruleModel, err := ruleModel(rule)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
		model.Rules = append(model.Rules, ruleModel)
	}
	for _, condition := range permissions.Conditions {
		var conditionModel ConditionModel
		conditionModel.IpFilter.AllowedCidrRanges = make([]types.String, 0, len(condition.IpFilter.AllowedCidrRanges))
		for _, cidr := range condition.IpFilter.AllowedCidrRanges {
			conditionModel.IpFilter.AllowedCidrRanges = append(conditionModel.IpFilter.AllowedCidrRanges, types.StringValue(cidr))
		}
		model.Conditions = append(model.Conditions, conditionModel)
	}
	return model, nil
}

func (r *CustomRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CustomRoleResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Find custom role
	client := *r.httpClient
	foundCustomRole, err := describeCustomRole(client, state.Id.ValueStringPointer(), state.Name.ValueStringPointer(), r.httpMgaEndpoint, r.httpAuthToken)
	if foundCustomRole == nil && err == nil {
		// Custom role not found, remove from state
		identifierString := fmt.Sprintf("name \"%s\"", state.Name.ValueString())
		if !state.Id.IsNull() {
			// Prioritize ID if available; name can change
			identifierString = fmt.Sprintf("ID \"%s\"", state.Id.ValueString())
		}
		resp.Diagnostics.AddWarning("Custom Role Not Found", fmt.Sprintf("The custom role with %s was not found. It may have been deleted outside of Terraform. Removing from state.", identifierString))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read custom role, got error: %s", err))
		return
	}

	permissions, err := permissionsModel(foundCustomRole.Permissions)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to parse custom role permissions, got error: %s", err))
		return
	}
	state.Id = types.StringValue(foundCustomRole.Id)
	state.Name = types.StringValue(foundCustomRole.Name)
	state.Description = types.StringValue(foundCustomRole.Description)
	state.Permissions = permissions

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, CustomRoleIdentityModel{Name: state.Name})...)
}

func (r *CustomRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan CustomRoleResourceModel

	// Retrieve values from the plan
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	client := *r.httpClient
	putUrl := fmt.Sprintf("%s/roles/%s", r.httpMgaEndpoint, plan.Id.ValueString())

	requestMap := CustomRoleData{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	}
	if plan.Permissions.Rules != nil {
		requestMap.Permissions.Rules = make([]RuleData, 0, len(plan.Permissions.Rules))
		for _, rule := range plan.Permissions.Rules {
			requestMap.Permissions.Rules = append(requestMap.Permissions.Rules, ruleData(rule))
		}
	}
	if plan.Permissions.Conditions != nil {
		requestMap.Permissions.Conditions = make([]ConditionData, 0, len(plan.Permissions.Conditions))
		for _, condition := range plan.Permissions.Conditions {
			allowedCidrRanges := make([]string, len(condition.IpFilter.AllowedCidrRanges))
			for i, cidr := range condition.IpFilter.AllowedCidrRanges {
				allowedCidrRanges[i] = cidr.ValueString()
			}
			requestMap.Permissions.Conditions = append(requestMap.Permissions.Conditions,
				ConditionData{
					IpFilter: IpFilterData{
						AllowedCidrRanges: allowedCidrRanges,
					},
				},
			)
		}
	}

	requestJson, err := json.Marshal(requestMap)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to marshal request body, got error: %s", err))
		return
	}
	requestBody := bytes.NewBuffer(requestJson)
	putRequest, err := http.NewRequest("PUT", putUrl, requestBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create HTTP request to update custom role, got error: %s", err))
		return
	}
	putRequest.Header.Set("Content-Type", "application/json")
	putRequest.Header.Set("Authorization", r.httpAuthToken)
	httpResp, err := client.Do(putRequest)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update custom role, got error: %s", err))
		return
	}
	if httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(httpResp.Body)
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update custom role, got non-200 response: %s %s", httpResp.Status, string(body)))
		return
	}

	// Map response body to schema and populate computed attribute values
	plan.Id = types.StringValue(plan.Id.ValueString())

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, CustomRoleIdentityModel{Name: plan.Name})...)
}

func (r *CustomRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Imported by ID (`terraform import` or an import block with `id`): the role ID.
	if req.ID != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
		return
	}

	// Imported by an import block with `identity`: the role name.
	var identity CustomRoleIdentityModel
	resp.Diagnostics.Append(req.Identity.Get(ctx, &identity)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), identity.Name)...)
}

type ListCustomRolesData struct {
	Roles     []CustomRoleData `json:"roles"`
	NextToken *string          `json:"next_token"`
}

func listCustomRoles(client http.Client, httpMgaEndpoint string, httpAuthToken string, nextToken *string) (*ListCustomRolesData, error) {
	getUrl := fmt.Sprintf("%s/roles?type=custom", httpMgaEndpoint)
	if nextToken != nil {
		getUrl = fmt.Sprintf("%s&next_token=%v", getUrl, *nextToken)
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
		return nil, fmt.Errorf("unable to list custom roles, got non-2xx response: %s %s", getResp.Status, string(body))
	}

	bodyBytes, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	var customRolesList ListCustomRolesData
	err = json.Unmarshal(bodyBytes, &customRolesList)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return &customRolesList, nil
}

func describeCustomRole(client http.Client, id *string, name *string, httpMgaEndpoint string, httpAuthToken string) (*CustomRoleData, error) {
	var nextToken *string
	for {
		customRolesList, err := listCustomRoles(client, httpMgaEndpoint, httpAuthToken, nextToken)
		if err != nil {
			return nil, fmt.Errorf("error listing custom roles: %v", err)
		}
		for _, role := range customRolesList.Roles {
			if id != nil && role.Id == *id {
				return &role, nil
			}
			if id == nil {
				// Prioritize ID if available; name can change
				if name != nil && role.Name == *name {
					return &role, nil
				}
			}
		}
		nextToken = customRolesList.NextToken
		if nextToken == nil {
			break
		}
	}
	return nil, nil
}
