package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &ContainerClusterResource{}
	_ resource.ResourceWithConfigure = &ContainerClusterResource{}
)

type ContainerClusterResource struct {
	client *ApiClient
}

type ContainerClusterResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	VPCID            types.String `tfsdk:"vpc_id"`
	Region           types.String `tfsdk:"region"`
	CapacityProvider types.String `tfsdk:"capacity_provider"`
	Status           types.String `tfsdk:"status"`
}

func NewContainerClusterResource() resource.Resource {
	return &ContainerClusterResource{}
}

func (r *ContainerClusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_cluster"
}

func (r *ContainerClusterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud container cluster.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the container cluster.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the container cluster.",
				Required:    true,
			},
			"vpc_id": schema.StringAttribute{
				Description: "The VPC ID to place the container cluster in.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Description: "The region where the cluster will be created.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"capacity_provider": schema.StringAttribute{
				Description: "The capacity provider strategy (e.g. FARGATE, EC2).",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				Description: "The current status of the container cluster.",
				Computed:    true,
			},
		},
	}
}

func (r *ContainerClusterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ContainerClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ContainerClusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":  plan.Name.ValueString(),
		"vpcId": plan.VPCID.ValueString(),
	}

	if !plan.Region.IsNull() && !plan.Region.IsUnknown() {
		body["region"] = plan.Region.ValueString()
	}
	if !plan.CapacityProvider.IsNull() && !plan.CapacityProvider.IsUnknown() {
		body["capacityProvider"] = plan.CapacityProvider.ValueString()
	}

	respBody, statusCode, err := r.client.Post("/containers/clusters", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating container cluster", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating container cluster",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var apiResp map[string]interface{}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	// The API returns { success, data: { ... } }
	result, _ := apiResp["data"].(map[string]interface{})
	if result == nil {
		result = apiResp
	}

	plan.ID = types.StringValue(getString(result, "id"))
	plan.Status = types.StringValue(getString(result, "status"))
	if v := getString(result, "region"); v != "" {
		plan.Region = types.StringValue(v)
	}
	if v := getString(result, "capacityProvider"); v != "" {
		plan.CapacityProvider = types.StringValue(v)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ContainerClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ContainerClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/containers/clusters/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading container cluster", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading container cluster",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var apiResp map[string]interface{}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	result, _ := apiResp["data"].(map[string]interface{})
	if result == nil {
		result = apiResp
	}

	state.Name = types.StringValue(getString(result, "name"))
	state.VPCID = types.StringValue(getString(result, "vpcId"))
	state.Status = types.StringValue(getString(result, "status"))
	if v := getString(result, "region"); v != "" {
		state.Region = types.StringValue(v)
	}
	if v := getString(result, "capacityProvider"); v != "" {
		state.CapacityProvider = types.StringValue(v)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ContainerClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ContainerClusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ContainerClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name": plan.Name.ValueString(),
	}

	if !plan.CapacityProvider.IsNull() && !plan.CapacityProvider.IsUnknown() {
		body["capacityProvider"] = plan.CapacityProvider.ValueString()
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/containers/clusters/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating container cluster", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating container cluster",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var apiResp map[string]interface{}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	result, _ := apiResp["data"].(map[string]interface{})
	if result == nil {
		result = apiResp
	}

	plan.ID = state.ID
	plan.Status = types.StringValue(getString(result, "status"))
	if v := getString(result, "region"); v != "" {
		plan.Region = types.StringValue(v)
	}
	if v := getString(result, "capacityProvider"); v != "" {
		plan.CapacityProvider = types.StringValue(v)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ContainerClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ContainerClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/containers/clusters/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting container cluster", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting container cluster",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}
