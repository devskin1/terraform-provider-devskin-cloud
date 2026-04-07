package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &TaskDefinitionResource{}
	_ resource.ResourceWithConfigure = &TaskDefinitionResource{}
)

type TaskDefinitionResource struct {
	client *ApiClient
}

type PortMappingModel struct {
	ContainerPort types.Int64  `tfsdk:"container_port"`
	HostPort      types.Int64  `tfsdk:"host_port"`
	Protocol      types.String `tfsdk:"protocol"`
}

type TaskDefinitionResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Family           types.String `tfsdk:"family"`
	Image            types.String `tfsdk:"image"`
	CPU              types.Int64  `tfsdk:"cpu"`
	Memory           types.Int64  `tfsdk:"memory"`
	PortMappings     types.List   `tfsdk:"port_mappings"`
	Environment      types.Map    `tfsdk:"environment"`
	SourceRepository types.String `tfsdk:"source_repository"`
	SourceBranch     types.String `tfsdk:"source_branch"`
	Revision         types.Int64  `tfsdk:"revision"`
	Status           types.String `tfsdk:"status"`
}

var portMappingAttrTypes = map[string]attr.Type{
	"container_port": types.Int64Type,
	"host_port":      types.Int64Type,
	"protocol":       types.StringType,
}

func NewTaskDefinitionResource() resource.Resource {
	return &TaskDefinitionResource{}
}

func (r *TaskDefinitionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task_definition"
}

func (r *TaskDefinitionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud container task definition.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the task definition.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"family": schema.StringAttribute{
				Description: "The family name of the task definition.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"image": schema.StringAttribute{
				Description: "The container image to use (e.g. nginx:latest, myapp:v1.0).",
				Required:    true,
			},
			"cpu": schema.Int64Attribute{
				Description: "The number of CPU units to allocate (e.g. 256, 512, 1024).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(256),
			},
			"memory": schema.Int64Attribute{
				Description: "The amount of memory in MB to allocate (e.g. 512, 1024, 2048).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(512),
			},
			"port_mappings": schema.ListNestedAttribute{
				Description: "Port mappings for the container.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"container_port": schema.Int64Attribute{
							Description: "The port on the container.",
							Required:    true,
						},
						"host_port": schema.Int64Attribute{
							Description: "The port on the host. Defaults to the container port.",
							Optional:    true,
							Computed:    true,
						},
						"protocol": schema.StringAttribute{
							Description: "The protocol (tcp or udp). Defaults to tcp.",
							Optional:    true,
							Computed:    true,
							Default:     stringdefault.StaticString("tcp"),
						},
					},
				},
			},
			"environment": schema.MapAttribute{
				Description: "Environment variables to set in the container.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"source_repository": schema.StringAttribute{
				Description: "The source Git repository URL for building the image.",
				Optional:    true,
			},
			"source_branch": schema.StringAttribute{
				Description: "The branch to build from when using source_repository.",
				Optional:    true,
			},
			"revision": schema.Int64Attribute{
				Description: "The revision number of the task definition.",
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "The current status of the task definition.",
				Computed:    true,
			},
		},
	}
}

func (r *TaskDefinitionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TaskDefinitionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TaskDefinitionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"family": plan.Family.ValueString(),
		"image":  plan.Image.ValueString(),
		"cpu":    plan.CPU.ValueInt64(),
		"memory": plan.Memory.ValueInt64(),
	}

	// Port mappings
	if !plan.PortMappings.IsNull() && !plan.PortMappings.IsUnknown() {
		var portMappings []PortMappingModel
		resp.Diagnostics.Append(plan.PortMappings.ElementsAs(ctx, &portMappings, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		pmPayload := make([]map[string]interface{}, len(portMappings))
		for i, pm := range portMappings {
			pmPayload[i] = map[string]interface{}{
				"containerPort": pm.ContainerPort.ValueInt64(),
			}
			if !pm.HostPort.IsNull() && !pm.HostPort.IsUnknown() {
				pmPayload[i]["hostPort"] = pm.HostPort.ValueInt64()
			}
			if !pm.Protocol.IsNull() && !pm.Protocol.IsUnknown() {
				pmPayload[i]["protocol"] = pm.Protocol.ValueString()
			}
		}
		body["portMappings"] = pmPayload
	}

	// Environment
	if !plan.Environment.IsNull() && !plan.Environment.IsUnknown() {
		envMap := make(map[string]string)
		resp.Diagnostics.Append(plan.Environment.ElementsAs(ctx, &envMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		body["environment"] = envMap
	}

	if !plan.SourceRepository.IsNull() && !plan.SourceRepository.IsUnknown() {
		body["sourceRepository"] = plan.SourceRepository.ValueString()
	}
	if !plan.SourceBranch.IsNull() && !plan.SourceBranch.IsUnknown() {
		body["sourceBranch"] = plan.SourceBranch.ValueString()
	}

	respBody, statusCode, err := r.client.Post("/containers/task-definitions", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating task definition", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating task definition",
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

	plan.ID = types.StringValue(getString(result, "id"))
	plan.Revision = types.Int64Value(getInt64(result, "revision"))
	plan.Status = types.StringValue(getString(result, "status"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *TaskDefinitionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TaskDefinitionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/containers/task-definitions/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading task definition", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading task definition",
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

	state.Family = types.StringValue(getString(result, "family"))
	state.Image = types.StringValue(getString(result, "image"))
	state.CPU = types.Int64Value(getInt64(result, "cpu"))
	state.Memory = types.Int64Value(getInt64(result, "memory"))
	state.Revision = types.Int64Value(getInt64(result, "revision"))
	state.Status = types.StringValue(getString(result, "status"))

	if v := getString(result, "sourceRepository"); v != "" {
		state.SourceRepository = types.StringValue(v)
	}
	if v := getString(result, "sourceBranch"); v != "" {
		state.SourceBranch = types.StringValue(v)
	}

	// Parse port_mappings from response
	if rawPMs, ok := result["portMappings"].([]interface{}); ok && len(rawPMs) > 0 {
		pmValues := make([]attr.Value, len(rawPMs))
		for i, rawPM := range rawPMs {
			if pm, ok := rawPM.(map[string]interface{}); ok {
				hostPort := getInt64(pm, "hostPort")
				if hostPort == 0 {
					hostPort = getInt64(pm, "containerPort")
				}
				pmValues[i], _ = types.ObjectValue(portMappingAttrTypes, map[string]attr.Value{
					"container_port": types.Int64Value(getInt64(pm, "containerPort")),
					"host_port":      types.Int64Value(hostPort),
					"protocol":       types.StringValue(getString(pm, "protocol")),
				})
			}
		}
		pmList, diags := types.ListValue(types.ObjectType{AttrTypes: portMappingAttrTypes}, pmValues)
		resp.Diagnostics.Append(diags...)
		state.PortMappings = pmList
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

func (r *TaskDefinitionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan TaskDefinitionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state TaskDefinitionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"family": plan.Family.ValueString(),
		"image":  plan.Image.ValueString(),
		"cpu":    plan.CPU.ValueInt64(),
		"memory": plan.Memory.ValueInt64(),
	}

	// Port mappings
	if !plan.PortMappings.IsNull() && !plan.PortMappings.IsUnknown() {
		var portMappings []PortMappingModel
		resp.Diagnostics.Append(plan.PortMappings.ElementsAs(ctx, &portMappings, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		pmPayload := make([]map[string]interface{}, len(portMappings))
		for i, pm := range portMappings {
			pmPayload[i] = map[string]interface{}{
				"containerPort": pm.ContainerPort.ValueInt64(),
			}
			if !pm.HostPort.IsNull() && !pm.HostPort.IsUnknown() {
				pmPayload[i]["hostPort"] = pm.HostPort.ValueInt64()
			}
			if !pm.Protocol.IsNull() && !pm.Protocol.IsUnknown() {
				pmPayload[i]["protocol"] = pm.Protocol.ValueString()
			}
		}
		body["portMappings"] = pmPayload
	}

	// Environment
	if !plan.Environment.IsNull() && !plan.Environment.IsUnknown() {
		envMap := make(map[string]string)
		resp.Diagnostics.Append(plan.Environment.ElementsAs(ctx, &envMap, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		body["environment"] = envMap
	}

	if !plan.SourceRepository.IsNull() && !plan.SourceRepository.IsUnknown() {
		body["sourceRepository"] = plan.SourceRepository.ValueString()
	}
	if !plan.SourceBranch.IsNull() && !plan.SourceBranch.IsUnknown() {
		body["sourceBranch"] = plan.SourceBranch.ValueString()
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/containers/task-definitions/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating task definition", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating task definition",
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
	plan.Revision = types.Int64Value(getInt64(result, "revision"))
	plan.Status = types.StringValue(getString(result, "status"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *TaskDefinitionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TaskDefinitionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/containers/task-definitions/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting task definition", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting task definition",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}
