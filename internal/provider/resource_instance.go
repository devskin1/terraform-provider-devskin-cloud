package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &InstanceResource{}
	_ resource.ResourceWithConfigure = &InstanceResource{}
)

type InstanceResource struct {
	client *ApiClient
}

type InstanceResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	InstanceType types.String `tfsdk:"instance_type"`
	ImageID      types.String `tfsdk:"image_id"`
	Region       types.String `tfsdk:"region"`
	VPCID        types.String `tfsdk:"vpc_id"`
	SubnetID     types.String `tfsdk:"subnet_id"`
	IPv6         types.Bool   `tfsdk:"ipv6"`
	Status       types.String `tfsdk:"status"`
	PublicIP     types.String `tfsdk:"public_ip"`
	PrivateIP    types.String `tfsdk:"private_ip"`
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
				Description: "The name of the instance.",
				Required:    true,
			},
			"instance_type": schema.StringAttribute{
				Description: "The instance type (e.g. ds.small, ds.medium, ds.large).",
				Required:    true,
			},
			"image_id": schema.StringAttribute{
				Description: "The ID of the image to use for the instance.",
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

func (r *InstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan InstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":          plan.Name.ValueString(),
		"instance_type": plan.InstanceType.ValueString(),
		"image_id":      plan.ImageID.ValueString(),
		"region":        plan.Region.ValueString(),
		"vpc_id":        plan.VPCID.ValueString(),
		"subnet_id":     plan.SubnetID.ValueString(),
		"ipv6":          plan.IPv6.ValueBool(),
	}

	respBody, statusCode, err := r.client.Post("/instances", body)
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

	plan.ID = types.StringValue(getString(result, "id"))
	plan.Status = types.StringValue(getString(result, "status"))
	plan.PublicIP = types.StringValue(getString(result, "public_ip"))
	plan.PrivateIP = types.StringValue(getString(result, "private_ip"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *InstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state InstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/instances/%s", state.ID.ValueString()))
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

	state.Name = types.StringValue(getString(result, "name"))
	state.InstanceType = types.StringValue(getString(result, "instance_type"))
	state.ImageID = types.StringValue(getString(result, "image_id"))
	state.Region = types.StringValue(getString(result, "region"))
	state.VPCID = types.StringValue(getString(result, "vpc_id"))
	state.SubnetID = types.StringValue(getString(result, "subnet_id"))
	state.IPv6 = types.BoolValue(getBool(result, "ipv6"))
	state.Status = types.StringValue(getString(result, "status"))
	state.PublicIP = types.StringValue(getString(result, "public_ip"))
	state.PrivateIP = types.StringValue(getString(result, "private_ip"))

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
		"name":          plan.Name.ValueString(),
		"instance_type": plan.InstanceType.ValueString(),
		"ipv6":          plan.IPv6.ValueBool(),
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/instances/%s", state.ID.ValueString()), body)
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

	plan.ID = state.ID
	plan.Status = types.StringValue(getString(result, "status"))
	plan.PublicIP = types.StringValue(getString(result, "public_ip"))
	plan.PrivateIP = types.StringValue(getString(result, "private_ip"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *InstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state InstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/instances/%s", state.ID.ValueString()))
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
