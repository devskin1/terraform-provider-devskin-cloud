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
	_ resource.Resource              = &DatabaseResource{}
	_ resource.ResourceWithConfigure = &DatabaseResource{}
)

type DatabaseResource struct {
	client *ApiClient
}

type DatabaseResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Engine        types.String `tfsdk:"engine"`
	InstanceClass types.String `tfsdk:"instance_class"`
	Storage       types.Int64  `tfsdk:"storage"`
	VPCID         types.String `tfsdk:"vpc_id"`
	Status        types.String `tfsdk:"status"`
	Endpoint      types.String `tfsdk:"endpoint"`
	Port          types.Int64  `tfsdk:"port"`
}

func NewDatabaseResource() resource.Resource {
	return &DatabaseResource{}
}

func (r *DatabaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *DatabaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud managed database instance.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the database.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the database instance.",
				Required:    true,
			},
			"engine": schema.StringAttribute{
				Description: "The database engine (e.g. postgres, mysql, redis, mongodb).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"instance_class": schema.StringAttribute{
				Description: "The instance class for the database (e.g. db.small, db.medium, db.large).",
				Required:    true,
			},
			"storage": schema.Int64Attribute{
				Description: "The allocated storage in GB.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(20),
			},
			"vpc_id": schema.StringAttribute{
				Description: "The VPC ID to place the database in.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Description: "The current status of the database.",
				Computed:    true,
			},
			"endpoint": schema.StringAttribute{
				Description: "The connection endpoint for the database.",
				Computed:    true,
			},
			"port": schema.Int64Attribute{
				Description: "The connection port for the database.",
				Computed:    true,
			},
		},
	}
}

func (r *DatabaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":           plan.Name.ValueString(),
		"engine":         plan.Engine.ValueString(),
		"instance_class": plan.InstanceClass.ValueString(),
		"storage":        plan.Storage.ValueInt64(),
		"vpc_id":         plan.VPCID.ValueString(),
	}

	respBody, statusCode, err := r.client.Post("/databases", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating database", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating database",
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
	plan.Endpoint = types.StringValue(getString(result, "endpoint"))
	plan.Port = types.Int64Value(getInt64(result, "port"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/databases/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading database", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading database",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	state.Name = types.StringValue(getString(result, "name"))
	state.Engine = types.StringValue(getString(result, "engine"))
	state.InstanceClass = types.StringValue(getString(result, "instance_class"))
	state.Storage = types.Int64Value(getInt64(result, "storage"))
	state.VPCID = types.StringValue(getString(result, "vpc_id"))
	state.Status = types.StringValue(getString(result, "status"))
	state.Endpoint = types.StringValue(getString(result, "endpoint"))
	state.Port = types.Int64Value(getInt64(result, "port"))

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *DatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state DatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":           plan.Name.ValueString(),
		"instance_class": plan.InstanceClass.ValueString(),
		"storage":        plan.Storage.ValueInt64(),
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/databases/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating database", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating database",
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
	plan.Endpoint = types.StringValue(getString(result, "endpoint"))
	plan.Port = types.Int64Value(getInt64(result, "port"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/databases/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting database", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting database",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case json.Number:
			i, _ := n.Int64()
			return i
		}
	}
	return 0
}
