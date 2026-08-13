package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &InstanceResource{}
	_ resource.ResourceWithConfigure   = &InstanceResource{}
	_ resource.ResourceWithImportState = &InstanceResource{}
)

type InstanceResource struct {
	client *ApiClient
}

// Attribute names are snake_case for HCL ergonomics; the request bodies
// translate to the backend's camelCase keys (instanceType, imageId, ...).
// Sending snake_case straight through fails validation with
// {"instanceType":["Required"],"imageId":["Required"],...}.
type InstanceResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	InstanceType         types.String `tfsdk:"instance_type"`
	ImageID              types.String `tfsdk:"image_id"`
	Region               types.String `tfsdk:"region"`
	VPCID                types.String `tfsdk:"vpc_id"`
	SubnetID             types.String `tfsdk:"subnet_id"`
	IPv6                 types.Bool   `tfsdk:"ipv6"`
	SecurityGroupIDs     types.List   `tfsdk:"security_group_ids"`
	KeyPairID            types.String `tfsdk:"key_pair_id"`
	VolumeSize           types.Int64  `tfsdk:"volume_size"`
	VolumeType           types.String `tfsdk:"volume_type"`
	AssignPublicIP       types.Bool   `tfsdk:"assign_public_ip"`
	Tags                 types.Map    `tfsdk:"tags"`
	Status               types.String `tfsdk:"status"`
	PublicIP             types.String `tfsdk:"public_ip"`
	PrivateIP            types.String `tfsdk:"private_ip"`
	MonitoringEnrollment types.Object `tfsdk:"monitoring_enrollment"`
}

func NewInstanceResource() resource.Resource {
	return &InstanceResource{}
}

func (r *InstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

func (r *InstanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud compute instance.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the instance.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the instance. Letters, numbers and hyphens only.",
				Required:    true,
			},
			"instance_type": schema.StringAttribute{
				Description: "The instance type (e.g. c5.large, c5.xlarge, c5.4c16). Must exist and be available in the catalog — AWS names like t3.medium are registered but disabled.",
				Required:    true,
			},
			"image_id": schema.StringAttribute{
				Description: "The template to boot from (e.g. tpl-9100 for Ubuntu).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Description: "The region where the instance will be created.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Description: "The VPC ID to place the instance in.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"subnet_id": schema.StringAttribute{
				Description: "The subnet ID within the VPC.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ipv6": schema.BoolAttribute{
				Description: "Whether to assign an IPv6 address.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"security_group_ids": schema.ListAttribute{
				Description:   "Security groups to attach. Omit to use the VPC default.",
				Optional:      true,
				ElementType:   types.StringType,
				PlanModifiers: []planmodifier.List{},
			},
			"key_pair_id": schema.StringAttribute{
				Description: "SSH key pair ID to inject at boot.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_size": schema.Int64Attribute{
				Description: "Root volume size in GB (20-16384). Defaults to 25 when omitted.",
				Optional:    true,
			},
			"volume_type": schema.StringAttribute{
				Description: "Root volume type (e.g. gp3). Defaults to gp3.",
				Optional:    true,
			},
			"assign_public_ip": schema.BoolAttribute{
				Description: "Whether to allocate a public IP at creation. Defaults to true.",
				Optional:    true,
			},
			"tags": schema.MapAttribute{
				Description: "Key/value tags applied to the instance.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"status": schema.StringAttribute{
				Description: "The current status of the instance.",
				Computed:    true,
			},
			"public_ip": schema.StringAttribute{
				Description: "The public IP address assigned to the instance.",
				Computed:    true,
			},
			"private_ip": schema.StringAttribute{
				Description: "The private IP address assigned to the instance.",
				Computed:    true,
			},
			"monitoring_enrollment": schema.SingleNestedAttribute{
				Description: "Optional Flux observability enrollment. Only consumed at create time — modifying this block on an existing instance is a no-op.",
				Optional:    true,
				Attributes: map[string]schema.Attribute{
					"enabled": schema.BoolAttribute{
						Description: "Enroll the VM into Flux observability at boot.",
						Required:    true,
					},
					"api_key": schema.StringAttribute{
						Description: "Flux project API key. Required when enabled is true.",
						Optional:    true,
						Sensitive:   true,
					},
				},
			},
		},
	}
}

func (r *InstanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*ApiClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *ApiClient, got something else.")
		return
	}
	r.client = client
}

// buildCreateBody maps the HCL attributes onto createInstanceSchema
// (camelCase keys) in compute.controller.ts.
func (r *InstanceResource) buildCreateBody(ctx context.Context, plan InstanceResourceModel) map[string]interface{} {
	body := map[string]interface{}{
		"name":         plan.Name.ValueString(),
		"instanceType": plan.InstanceType.ValueString(),
		"imageId":      plan.ImageID.ValueString(),
		"region":       plan.Region.ValueString(),
		"vpcId":        plan.VPCID.ValueString(),
		"subnetId":     plan.SubnetID.ValueString(),
		"ipv6":         plan.IPv6.ValueBool(),
	}

	if !plan.SecurityGroupIDs.IsNull() && !plan.SecurityGroupIDs.IsUnknown() {
		var ids []string
		plan.SecurityGroupIDs.ElementsAs(ctx, &ids, false)
		if len(ids) > 0 {
			body["securityGroupIds"] = ids
		}
	}
	if !plan.KeyPairID.IsNull() && !plan.KeyPairID.IsUnknown() {
		body["keyPairId"] = plan.KeyPairID.ValueString()
	}
	if !plan.VolumeSize.IsNull() && !plan.VolumeSize.IsUnknown() {
		body["volumeSize"] = plan.VolumeSize.ValueInt64()
	}
	if !plan.VolumeType.IsNull() && !plan.VolumeType.IsUnknown() {
		body["volumeType"] = plan.VolumeType.ValueString()
	}
	if !plan.AssignPublicIP.IsNull() && !plan.AssignPublicIP.IsUnknown() {
		body["publicIp"] = plan.AssignPublicIP.ValueBool()
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		var tags map[string]string
		plan.Tags.ElementsAs(ctx, &tags, false)
		if len(tags) > 0 {
			body["tags"] = tags
		}
	}

	// Optional Flux enrollment — only consumed at create time. Boolean
	// `monitoring: true` flips the legacy flag the backend already understood;
	// the structured `monitoringEnrollment` carries the api key.
	if !plan.MonitoringEnrollment.IsNull() && !plan.MonitoringEnrollment.IsUnknown() {
		attrs := plan.MonitoringEnrollment.Attributes()
		enabled := false
		if v, ok := attrs["enabled"].(types.Bool); ok {
			enabled = v.ValueBool()
		}
		if enabled {
			body["monitoring"] = true
			enrollment := map[string]interface{}{"enabled": true}
			if v, ok := attrs["api_key"].(types.String); ok && !v.IsNull() && !v.IsUnknown() {
				enrollment["apiKey"] = v.ValueString()
			}
			body["monitoringEnrollment"] = enrollment
		}
	}
	return body
}

// unwrap pulls the payload out of the `{ success, data }` envelope the backend
// uses on most routes. Reading the wrapper directly yields empty IDs.
func unwrapData(result map[string]interface{}) map[string]interface{} {
	if data, ok := result["data"].(map[string]interface{}); ok {
		return data
	}
	return result
}

// applyRemote copies the API response (camelCase) onto the model.
func applyInstanceRemote(m *InstanceResourceModel, result map[string]interface{}) {
	m.ID = types.StringValue(getString(result, "id"))
	m.Status = types.StringValue(getString(result, "status"))
	m.PublicIP = types.StringValue(getString(result, "publicIp"))
	m.PrivateIP = types.StringValue(getString(result, "privateIp"))
}

func (r *InstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan InstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Post("/compute/instances", r.buildCreateBody(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating instance", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating instance",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyInstanceRemote(&plan, unwrapData(result))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *InstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state InstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/compute/instances/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading instance", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading instance",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	data := unwrapData(result)

	// Backend keys are camelCase. The previous version read snake_case here,
	// which never matched and reported permanent drift.
	state.Name = types.StringValue(getString(data, "name"))
	state.InstanceType = types.StringValue(getString(data, "instanceType"))
	state.ImageID = types.StringValue(getString(data, "imageId"))
	state.Region = types.StringValue(getString(data, "region"))
	state.VPCID = types.StringValue(getString(data, "vpcId"))
	state.SubnetID = types.StringValue(getString(data, "subnetId"))
	state.IPv6 = types.BoolValue(getBool(data, "ipv6"))
	applyInstanceRemote(&state, data)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *InstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan InstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state InstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":         plan.Name.ValueString(),
		"instanceType": plan.InstanceType.ValueString(),
		"ipv6":         plan.IPv6.ValueBool(),
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/compute/instances/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating instance", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating instance",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	data := unwrapData(result)

	plan.ID = state.ID
	plan.Status = types.StringValue(getString(data, "status"))
	plan.PublicIP = types.StringValue(getString(data, "publicIp"))
	plan.PrivateIP = types.StringValue(getString(data, "privateIp"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *InstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state InstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/compute/instances/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting instance", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting instance",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}

// --- Helpers ---

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ImportState permite `terraform import` pelo ID do recurso — essencial para
// readotar infra existente ou recuperar um state perdido, que antes exigia
// destruir tudo na mao e recriar.
func (r *InstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
