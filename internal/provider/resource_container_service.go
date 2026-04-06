package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &ContainerServiceResource{}
	_ resource.ResourceWithConfigure = &ContainerServiceResource{}
)

type ContainerServiceResource struct {
	client *ApiClient
}

type ContainerServiceResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Image            types.String `tfsdk:"image"`
	Port             types.Int64  `tfsdk:"port"`
	DesiredCount     types.Int64  `tfsdk:"desired_count"`
	SourceRepository types.String `tfsdk:"source_repository"`
	Environment      types.Map    `tfsdk:"environment"`
	Status           types.String `tfsdk:"status"`
	URL              types.String `tfsdk:"url"`
}

func NewContainerServiceResource() resource.Resource {
	return &ContainerServiceResource{}
}

func (r *ContainerServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_service"
}

func (r *ContainerServiceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud container service (CloudFeet/ECS).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the container service.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the container service.",
				Required:    true,
			},
			"image": schema.StringAttribute{
				Description: "The container image to deploy (e.g. nginx:latest, myapp:v1.0).",
				Optional:    true,
			},
			"port": schema.Int64Attribute{
				Description: "The port the container listens on.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(80),
			},
			"desired_count": schema.Int64Attribute{
				Description: "The desired number of running container instances.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
			},
			"source_repository": schema.StringAttribute{
				Description: "The source Git repository URL for auto-deploy.",
				Optional:    true,
			},
			"environment": schema.MapAttribute{
				Description: "Environment variables to set in the container.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"status": schema.StringAttribute{
				Description: "The current status of the container service.",
				Computed:    true,
			},
			"url": schema.StringAttribute{
				Description: "The public URL of the deployed container service.",
				Computed:    true,
			},
		},
	}
}

func (r *ContainerServiceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ContainerServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ContainerServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":          plan.Name.ValueString(),
		"port":          plan.Port.ValueInt64(),
		"desired_count": plan.DesiredCount.ValueInt64(),
	}

	if !plan.Image.IsNull() && !plan.Image.IsUnknown() {
		body["image"] = plan.Image.ValueString()
	}
	if !plan.SourceRepository.IsNull() && !plan.SourceRepository.IsUnknown() {
		body["source_repository"] = plan.SourceRepository.ValueString()
	}

	if !plan.Environment.IsNull() && !plan.Environment.IsUnknown() {
		envMap := make(map[string]string)
		resp.Diagnostics.Append(plan.Environment.ElementsAs(ctx, &envMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		body["environment"] = envMap
	}

	respBody, statusCode, err := r.client.Post("/container-services", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating container service", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating container service",
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
	plan.URL = types.StringValue(getString(result, "url"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ContainerServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ContainerServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/container-services/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading container service", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading container service",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	state.Name = types.StringValue(getString(result, "name"))
	state.Port = types.Int64Value(getInt64(result, "port"))
	state.DesiredCount = types.Int64Value(getInt64(result, "desired_count"))
	state.Status = types.StringValue(getString(result, "status"))
	state.URL = types.StringValue(getString(result, "url"))

	if v := getString(result, "image"); v != "" {
		state.Image = types.StringValue(v)
	}
	if v := getString(result, "source_repository"); v != "" {
		state.SourceRepository = types.StringValue(v)
	}

	// Parse environment from response
	if envRaw, ok := result["environment"].(map[string]interface{}); ok {
		envMap := make(map[string]string)
		for k, v := range envRaw {
			envMap[k] = fmt.Sprintf("%v", v)
		}
		envValue, diags := types.MapValueFrom(ctx, types.StringType, envMap)
		resp.Diagnostics.Append(diags...)
		state.Environment = envValue
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ContainerServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ContainerServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ContainerServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":          plan.Name.ValueString(),
		"port":          plan.Port.ValueInt64(),
		"desired_count": plan.DesiredCount.ValueInt64(),
	}

	if !plan.Image.IsNull() && !plan.Image.IsUnknown() {
		body["image"] = plan.Image.ValueString()
	}
	if !plan.SourceRepository.IsNull() && !plan.SourceRepository.IsUnknown() {
		body["source_repository"] = plan.SourceRepository.ValueString()
	}

	if !plan.Environment.IsNull() && !plan.Environment.IsUnknown() {
		envMap := make(map[string]string)
		resp.Diagnostics.Append(plan.Environment.ElementsAs(ctx, &envMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		body["environment"] = envMap
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/container-services/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating container service", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating container service",
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
	plan.URL = types.StringValue(getString(result, "url"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ContainerServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ContainerServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/container-services/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting container service", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting container service",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}
