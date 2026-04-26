package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &IAMRoleResource{}
	_ resource.ResourceWithConfigure = &IAMRoleResource{}
)

type IAMRoleResource struct {
	client *ApiClient
}

type IRSATrustEntryModel struct {
	ClusterID          types.String `tfsdk:"cluster_id"`
	Namespace          types.String `tfsdk:"namespace"`
	ServiceAccountName types.String `tfsdk:"service_account_name"`
}

type IAMRoleResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	Policies           types.List   `tfsdk:"policies"`
	IRSATrust          types.List   `tfsdk:"irsa_trust"`
	MaxSessionDuration types.Int64  `tfsdk:"max_session_duration"`
}

var irsaTrustAttrTypes = map[string]attr.Type{
	"cluster_id":           types.StringType,
	"namespace":            types.StringType,
	"service_account_name": types.StringType,
}

func NewIAMRoleResource() resource.Resource {
	return &IAMRoleResource{}
}

func (r *IAMRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_role"
}

func (r *IAMRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud IAM Role. Use `irsa_trust` to grant the role to a Kubernetes ServiceAccount via IRSA (OIDC federation).",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"policies": schema.ListAttribute{
				Description: "Platform policy actions, e.g. [\"s3:GetObject\", \"s3:ListBucket\"].",
				ElementType: types.StringType,
				Required:    true,
			},
			"irsa_trust": schema.ListNestedAttribute{
				Description: "Allowed Kubernetes ServiceAccounts that may assume this role via IRSA.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"cluster_id":           schema.StringAttribute{Required: true},
						"namespace":            schema.StringAttribute{Required: true},
						"service_account_name": schema.StringAttribute{Required: true, Description: "Use \"*\" to allow any SA in the namespace."},
					},
				},
			},
			"max_session_duration": schema.Int64Attribute{
				Description: "Max session duration in seconds (900-43200, default 3600).",
				Optional:    true,
				Computed:    true,
			},
		},
	}
}

func (r *IAMRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*ApiClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *ApiClient.")
		return
	}
	r.client = client
}

func (r *IAMRoleResource) buildBody(ctx context.Context, plan IAMRoleResourceModel, diags *[]error) map[string]interface{} {
	body := map[string]interface{}{
		"name": plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		body["description"] = plan.Description.ValueString()
	}
	if !plan.MaxSessionDuration.IsNull() && !plan.MaxSessionDuration.IsUnknown() {
		body["maxSessionDuration"] = plan.MaxSessionDuration.ValueInt64()
	}
	// Policies
	var pols []string
	plan.Policies.ElementsAs(ctx, &pols, false)
	body["policies"] = pols
	// IRSA trust
	if !plan.IRSATrust.IsNull() && !plan.IRSATrust.IsUnknown() {
		var trust []IRSATrustEntryModel
		plan.IRSATrust.ElementsAs(ctx, &trust, false)
		k8sList := make([]map[string]string, 0, len(trust))
		for _, t := range trust {
			k8sList = append(k8sList, map[string]string{
				"clusterId":          t.ClusterID.ValueString(),
				"namespace":          t.Namespace.ValueString(),
				"serviceAccountName": t.ServiceAccountName.ValueString(),
			})
		}
		body["trustPolicy"] = map[string]interface{}{"kubernetes": k8sList}
	}
	return body
}

func (r *IAMRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan IAMRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.buildBody(ctx, plan, nil)
	respBody, statusCode, err := r.client.Post("/iam/roles", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating IAM role", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating IAM role", fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)
	plan.ID = types.StringValue(getString(result, "id"))
	if plan.MaxSessionDuration.IsNull() || plan.MaxSessionDuration.IsUnknown() {
		plan.MaxSessionDuration = types.Int64Value(getInt64(result, "maxSessionDuration"))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *IAMRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state IAMRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/iam/roles/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading IAM role", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading IAM role", fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)
	state.Name = types.StringValue(getString(result, "name"))
	if v := getString(result, "description"); v != "" {
		state.Description = types.StringValue(v)
	}
	state.MaxSessionDuration = types.Int64Value(getInt64(result, "maxSessionDuration"))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *IAMRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan IAMRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state IAMRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.buildBody(ctx, plan, nil)
	_, statusCode, err := r.client.Patch(fmt.Sprintf("/iam/roles/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating IAM role", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating IAM role", fmt.Sprintf("Status %d", statusCode))
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *IAMRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state IAMRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, statusCode, err := r.client.Delete(fmt.Sprintf("/iam/roles/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting IAM role", err.Error())
		return
	}
	if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
		resp.Diagnostics.AddError("API error deleting IAM role", fmt.Sprintf("Status %d", statusCode))
	}
}

// Helper used internally — kept here to make the file self-contained.
var _ = irsaTrustAttrTypes
