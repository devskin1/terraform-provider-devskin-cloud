package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &FlexServiceResource{}
	_ resource.ResourceWithConfigure = &FlexServiceResource{}
)

type FlexServiceResource struct {
	client *ApiClient
}

type FlexServiceResourceModel struct {
	ID                 types.String  `tfsdk:"id"`
	Name               types.String  `tfsdk:"name"`
	SourceType         types.String  `tfsdk:"source_type"`
	SourceRepoUrl      types.String  `tfsdk:"source_repo_url"`
	SourceBranch       types.String  `tfsdk:"source_branch"`
	SourceImage        types.String  `tfsdk:"source_image"`
	Port               types.Int64   `tfsdk:"port"`
	Cpu                types.Float64 `tfsdk:"cpu"`
	Memory             types.Int64   `tfsdk:"memory"`
	MinInstances       types.Int64   `tfsdk:"min_instances"`
	MaxInstances       types.Int64   `tfsdk:"max_instances"`
	Concurrency        types.Int64   `tfsdk:"concurrency"`
	AutoscalingEnabled types.Bool    `tfsdk:"autoscaling_enabled"`
	EnvVars            types.Map     `tfsdk:"env_vars"`
	Status             types.String  `tfsdk:"status"`
	Url                types.String  `tfsdk:"url"`
}

func NewFlexServiceResource() resource.Resource {
	return &FlexServiceResource{}
}

func (r *FlexServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_flex_service"
}

func (r *FlexServiceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud Flex service (Cloud Run-like managed app platform).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the Flex service.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the Flex service (unique per org, used in the URL).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_type": schema.StringAttribute{
				Description: "Source type: github | gitlab | bitbucket | internal_git | image | zip.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source_repo_url": schema.StringAttribute{
				Description: "The source Git repository URL (for git-based source types).",
				Optional:    true,
			},
			"source_branch": schema.StringAttribute{
				Description: "The Git branch to deploy from. Defaults to 'main'.",
				Optional:    true,
				Computed:    true,
			},
			"source_image": schema.StringAttribute{
				Description: "Container image reference (for source_type = 'image').",
				Optional:    true,
			},
			"port": schema.Int64Attribute{
				Description: "The port the application listens on.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(8080),
			},
			"cpu": schema.Float64Attribute{
				Description: "vCPU allocation per instance (0.25, 0.5, 1, 2, 4).",
				Optional:    true,
				Computed:    true,
				Default:     float64default.StaticFloat64(0.5),
			},
			"memory": schema.Int64Attribute{
				Description: "Memory allocation per instance in MB (256, 512, 1024, 2048, 4096, 8192).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(512),
			},
			"min_instances": schema.Int64Attribute{
				Description: "Minimum number of running instances (0 = scale to zero).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
			},
			"max_instances": schema.Int64Attribute{
				Description: "Maximum number of running instances.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(5),
			},
			"concurrency": schema.Int64Attribute{
				Description: "Concurrent requests per instance.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(80),
			},
			"autoscaling_enabled": schema.BoolAttribute{
				Description: "Whether autoscaling is enabled.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"env_vars": schema.MapAttribute{
				Description: "Environment variables set on each instance.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"status": schema.StringAttribute{
				Description: "Current status of the Flex service.",
				Computed:    true,
			},
			"url": schema.StringAttribute{
				Description: "Auto-generated public URL of the Flex service.",
				Computed:    true,
			},
		},
	}
}

func (r *FlexServiceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *FlexServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FlexServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":               plan.Name.ValueString(),
		"sourceType":         plan.SourceType.ValueString(),
		"port":               plan.Port.ValueInt64(),
		"cpu":                plan.Cpu.ValueFloat64(),
		"memory":             plan.Memory.ValueInt64(),
		"minInstances":       plan.MinInstances.ValueInt64(),
		"maxInstances":       plan.MaxInstances.ValueInt64(),
		"concurrency":        plan.Concurrency.ValueInt64(),
		"autoscalingEnabled": plan.AutoscalingEnabled.ValueBool(),
	}

	if !plan.SourceRepoUrl.IsNull() && !plan.SourceRepoUrl.IsUnknown() {
		body["sourceRepoUrl"] = plan.SourceRepoUrl.ValueString()
	}
	if !plan.SourceBranch.IsNull() && !plan.SourceBranch.IsUnknown() {
		body["sourceBranch"] = plan.SourceBranch.ValueString()
	}
	if !plan.SourceImage.IsNull() && !plan.SourceImage.IsUnknown() {
		body["sourceImage"] = plan.SourceImage.ValueString()
	}

	if !plan.EnvVars.IsNull() && !plan.EnvVars.IsUnknown() {
		envMap := make(map[string]string)
		resp.Diagnostics.Append(plan.EnvVars.ElementsAs(ctx, &envMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		body["envVars"] = envMap
	}

	respBody, statusCode, err := r.client.Post("/flex/services", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating flex service", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating flex service",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	// Handle { success, data } envelope
	if data, ok := result["data"].(map[string]interface{}); ok {
		result = data
	}

	r.applyResultToState(ctx, result, &plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *FlexServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FlexServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/flex/services/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading flex service", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading flex service",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	if data, ok := result["data"].(map[string]interface{}); ok {
		result = data
	}

	r.applyResultToState(ctx, result, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *FlexServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FlexServiceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state FlexServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// PATCH /flex/services/:id/scale — min/max/concurrency
	scaleBody := map[string]interface{}{
		"minInstances": plan.MinInstances.ValueInt64(),
		"maxInstances": plan.MaxInstances.ValueInt64(),
		"concurrency":  plan.Concurrency.ValueInt64(),
	}
	respBody, statusCode, err := r.client.Patch(fmt.Sprintf("/flex/services/%s/scale", id), scaleBody)
	if err != nil {
		resp.Diagnostics.AddError("Error scaling flex service", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error scaling flex service",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	// PATCH /flex/services/:id/env — env vars
	if !plan.EnvVars.IsNull() && !plan.EnvVars.IsUnknown() {
		envMap := make(map[string]string)
		resp.Diagnostics.Append(plan.EnvVars.ElementsAs(ctx, &envMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		envBody := map[string]interface{}{"envVars": envMap}
		respBody, statusCode, err = r.client.Patch(fmt.Sprintf("/flex/services/%s/env", id), envBody)
		if err != nil {
			resp.Diagnostics.AddError("Error updating flex service env vars", err.Error())
			return
		}
		if statusCode < 200 || statusCode >= 300 {
			resp.Diagnostics.AddError("API error updating flex service env vars",
				fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
			return
		}
	}

	// Re-read to get latest state
	respBody, statusCode, err = r.client.Get(fmt.Sprintf("/flex/services/%s", id))
	if err != nil {
		resp.Diagnostics.AddError("Error re-reading flex service after update", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error re-reading flex service",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	if data, ok := result["data"].(map[string]interface{}); ok {
		result = data
	}

	plan.ID = state.ID
	r.applyResultToState(ctx, result, &plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *FlexServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FlexServiceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/flex/services/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting flex service", err.Error())
		return
	}
	if statusCode == 404 {
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting flex service",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}

// applyResultToState maps an API response map into a FlexServiceResourceModel.
func (r *FlexServiceResource) applyResultToState(ctx context.Context, result map[string]interface{}, m *FlexServiceResourceModel) {
	if v := getString(result, "id"); v != "" {
		m.ID = types.StringValue(v)
	}
	if v := getString(result, "name"); v != "" {
		m.Name = types.StringValue(v)
	}
	if v := getString(result, "sourceType"); v != "" {
		m.SourceType = types.StringValue(v)
	}
	if v := getString(result, "sourceRepoUrl"); v != "" {
		m.SourceRepoUrl = types.StringValue(v)
	}
	if v := getString(result, "sourceBranch"); v != "" {
		m.SourceBranch = types.StringValue(v)
	}
	if v := getString(result, "sourceImage"); v != "" {
		m.SourceImage = types.StringValue(v)
	}
	m.Port = types.Int64Value(getInt64(result, "port"))
	m.Cpu = types.Float64Value(getFloat64(result, "cpu"))
	m.Memory = types.Int64Value(getInt64(result, "memory"))
	m.MinInstances = types.Int64Value(getInt64(result, "minInstances"))
	m.MaxInstances = types.Int64Value(getInt64(result, "maxInstances"))
	m.Concurrency = types.Int64Value(getInt64(result, "concurrency"))
	if v, ok := result["autoscalingEnabled"].(bool); ok {
		m.AutoscalingEnabled = types.BoolValue(v)
	}
	m.Status = types.StringValue(getString(result, "status"))
	m.Url = types.StringValue(getString(result, "url"))

	if envRaw, ok := result["envVars"].(map[string]interface{}); ok {
		envMap := make(map[string]string)
		for k, v := range envRaw {
			envMap[k] = fmt.Sprintf("%v", v)
		}
		envValue, d := types.MapValueFrom(ctx, types.StringType, envMap)
		if !d.HasError() {
			m.EnvVars = envValue
		}
	}
}

// getFloat64 extracts a float64 from a response map.
func getFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		case int64:
			return float64(n)
		case json.Number:
			f, _ := n.Float64()
			return f
		}
	}
	return 0
}
