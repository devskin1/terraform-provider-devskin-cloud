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

// DatabaseResourceModel mirrors the backend's createDatabaseSchema in
// packages/backend/src/controllers/database.controller.ts. Field names
// here are snake_case for HCL ergonomics; the Create/Update bodies
// translate to the backend's camelCase keys.
type DatabaseResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Engine         types.String `tfsdk:"engine"`
	EngineVersion  types.String `tfsdk:"engine_version"`
	InstanceClass  types.String `tfsdk:"instance_class"`
	Storage        types.Int64  `tfsdk:"storage"`
	Username       types.String `tfsdk:"username"`
	Password       types.String `tfsdk:"password"`
	VPCID          types.String `tfsdk:"vpc_id"`
	Region         types.String `tfsdk:"region"`
	MultiAz        types.Bool   `tfsdk:"multi_az"`
	Replicas       types.Int64  `tfsdk:"replicas"`
	Status         types.String `tfsdk:"status"`
	Endpoint       types.String `tfsdk:"endpoint"`
	ReaderEndpoint types.String `tfsdk:"reader_endpoint"`
	Port           types.Int64  `tfsdk:"port"`
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
				Description: "Engine: MYSQL | POSTGRESQL | MARIADB | MONGODB | REDIS.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"engine_version": schema.StringAttribute{
				Description: "Engine version (e.g. 8.0 for MySQL, 16 for PostgreSQL). Defaults to latest if omitted.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"instance_class": schema.StringAttribute{
				Description: "Instance class (e.g. db.t3.small).",
				Required:    true,
			},
			"storage": schema.Int64Attribute{
				Description: "Allocated storage in GB.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(25),
			},
			"username": schema.StringAttribute{
				Description: "Master user.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password": schema.StringAttribute{
				Description: "Master password (min 8 chars). Sensitive.",
				Required:    true,
				Sensitive:   true,
			},
			"vpc_id": schema.StringAttribute{
				Description: "VPC ID where the database lands.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Description: "Region slug (default sa-east-1).",
				Optional:    true,
				Computed:    true,
			},
			"multi_az": schema.BoolAttribute{
				Description: "HA failover flag (currently a tag; future: true HA).",
				Optional:    true,
				Computed:    true,
			},
			"replicas": schema.Int64Attribute{
				Description: "Read-replica count (0-5). SQL engines only (MYSQL/POSTGRESQL/MARIADB). Backend provisions N extra VMs configured as read-replicas. Ignored for MongoDB/Redis (use their own resources).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
			},
			"status": schema.StringAttribute{
				Description: "Current status.",
				Computed:    true,
			},
			"endpoint": schema.StringAttribute{
				Description: "Writer / primary connection endpoint.",
				Computed:    true,
			},
			"reader_endpoint": schema.StringAttribute{
				Description: "Reader endpoint -- points at the first replica when one exists.",
				Computed:    true,
			},
			"port": schema.Int64Attribute{
				Description: "Connection port.",
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

// buildCreateBody maps the TF resource model to the backend's
// createDatabaseSchema (camelCase keys, UPPERCASE engine enum).
func (r *DatabaseResource) buildCreateBody(plan DatabaseResourceModel) map[string]interface{} {
	engine := plan.Engine.ValueString()
	region := plan.Region.ValueString()
	if region == "" {
		region = "sa-east-1"
	}
	engineVersion := plan.EngineVersion.ValueString()
	if engineVersion == "" {
		switch engine {
		case "MYSQL":
			engineVersion = "8.0"
		case "POSTGRESQL":
			engineVersion = "16"
		case "MARIADB":
			engineVersion = "10.11"
		case "MONGODB":
			engineVersion = "6.0"
		case "REDIS":
			engineVersion = "7.0"
		}
	}
	body := map[string]interface{}{
		"name":          plan.Name.ValueString(),
		"region":        region,
		"engine":        engine,
		"engineVersion": engineVersion,
		"instanceClass": plan.InstanceClass.ValueString(),
		"storage":       plan.Storage.ValueInt64(),
		"username":      plan.Username.ValueString(),
		"password":      plan.Password.ValueString(),
		"vpcId":         plan.VPCID.ValueString(),
	}
	if !plan.MultiAz.IsNull() && !plan.MultiAz.IsUnknown() {
		body["multiAz"] = plan.MultiAz.ValueBool()
	}
	// Read-replicas are SQL-only; backend ignores for Mongo/Redis but we
	// still send it -- avoids stale state if engine changes later.
	if !plan.Replicas.IsNull() && !plan.Replicas.IsUnknown() {
		body["replicas"] = plan.Replicas.ValueInt64()
	}
	return body
}

func (r *DatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := r.buildCreateBody(plan)
	respBody, statusCode, err := r.client.Post("/database/instances", body)
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
	// The backend wraps responses in `{ success, data }` for some routes
	// and returns the bare object for others -- unwrap if present so the
	// reads below work either way.
	if data, ok := result["data"].(map[string]interface{}); ok {
		result = data
	}

	plan.ID = types.StringValue(getString(result, "id"))
	plan.Status = types.StringValue(getString(result, "status"))
	plan.Endpoint = types.StringValue(getString(result, "endpoint"))
	plan.ReaderEndpoint = types.StringValue(getString(result, "readerEndpoint"))
	plan.Port = types.Int64Value(getInt64(result, "port"))
	// Echo back the version we resolved so the plan/state stays clean.
	if plan.EngineVersion.IsUnknown() || plan.EngineVersion.IsNull() {
		plan.EngineVersion = types.StringValue(getString(result, "engineVersion"))
	}
	if plan.Region.IsUnknown() || plan.Region.IsNull() {
		plan.Region = types.StringValue(getString(result, "region"))
	}
	if plan.MultiAz.IsUnknown() || plan.MultiAz.IsNull() {
		plan.MultiAz = types.BoolValue(getBool(result, "multiAz"))
	}
	if plan.Replicas.IsUnknown() || plan.Replicas.IsNull() {
		plan.Replicas = types.Int64Value(getInt64(result, "replicas"))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/database/instances/%s", state.ID.ValueString()))
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
	if data, ok := result["data"].(map[string]interface{}); ok {
		result = data
	}

	// Backend keys are camelCase; the old version was reading
	// snake_case (instance_class / vpc_id) which never matched and
	// returned empty strings -- forcing TF to perpetually plan a
	// "diff" on every refresh.
	state.Name = types.StringValue(getString(result, "name"))
	state.Engine = types.StringValue(getString(result, "engine"))
	state.EngineVersion = types.StringValue(getString(result, "engineVersion"))
	state.InstanceClass = types.StringValue(getString(result, "instanceClass"))
	state.Storage = types.Int64Value(getInt64(result, "storage"))
	state.VPCID = types.StringValue(getString(result, "vpcId"))
	state.Region = types.StringValue(getString(result, "region"))
	state.MultiAz = types.BoolValue(getBool(result, "multiAz"))
	state.Replicas = types.Int64Value(getInt64(result, "replicas"))
	state.Status = types.StringValue(getString(result, "status"))
	state.Endpoint = types.StringValue(getString(result, "endpoint"))
	state.ReaderEndpoint = types.StringValue(getString(result, "readerEndpoint"))
	state.Port = types.Int64Value(getInt64(result, "port"))
	// Username doesn't come back on the GET (security); preserve from state.
	// Password is never echoed -- preserve from state too.

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

	// Only send fields the backend's updateDatabase accepts. Importantly:
	//   - replicas → backend triggers scale-up / scale-down (SQL only)
	//   - multiAz   → tag-only
	//   - storage   → only increases are accepted backend-side
	//   - password  → reset via QEMU guest agent on the master VM
	body := map[string]interface{}{
		"name":          plan.Name.ValueString(),
		"instanceClass": plan.InstanceClass.ValueString(),
		"storage":       plan.Storage.ValueInt64(),
		"multiAz":       plan.MultiAz.ValueBool(),
		"replicas":      plan.Replicas.ValueInt64(),
	}
	if !plan.Password.IsNull() && plan.Password.ValueString() != state.Password.ValueString() {
		body["password"] = plan.Password.ValueString()
	}

	// Backend uses PATCH (not PUT). Earlier provider versions sent PUT
	// which 404'd the route.
	respBody, statusCode, err := r.client.Patch(fmt.Sprintf("/database/instances/%s", state.ID.ValueString()), body)
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
	if data, ok := result["data"].(map[string]interface{}); ok {
		result = data
	}

	plan.ID = state.ID
	plan.Status = types.StringValue(getString(result, "status"))
	plan.Endpoint = types.StringValue(getString(result, "endpoint"))
	plan.ReaderEndpoint = types.StringValue(getString(result, "readerEndpoint"))
	plan.Port = types.Int64Value(getInt64(result, "port"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/database/instances/%s", state.ID.ValueString()))
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

// (getBool already exported by resource_instance.go in this package)
