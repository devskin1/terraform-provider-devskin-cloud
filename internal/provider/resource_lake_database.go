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
	_ resource.Resource              = &LakeDatabaseResource{}
	_ resource.ResourceWithConfigure = &LakeDatabaseResource{}
)

type LakeDatabaseResource struct {
	client *ApiClient
}

type LakeDatabaseResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	BucketID    types.String `tfsdk:"bucket_id"`
	S3Location  types.String `tfsdk:"s3_location"`
	PolarisName types.String `tfsdk:"polaris_name"`
}

func NewLakeDatabaseResource() resource.Resource {
	return &LakeDatabaseResource{}
}

func (r *LakeDatabaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_database"
}

func (r *LakeDatabaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinLake (Lakehouse) catalog database. Backed by Apache Polaris and an S3 bucket for table storage.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "The unique identifier of the lake database.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "Catalog database name (must be unique within the org).",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Description: "Optional description.",
				Optional:    true,
			},
			"bucket_id": schema.StringAttribute{
				Description:   "Optional S3 bucket id to back the database. If omitted, the platform provisions one.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"s3_location": schema.StringAttribute{
				Description: "S3 URI prefix backing the database (e.g. s3://bucket/path).",
				Computed:    true,
			},
			"polaris_name": schema.StringAttribute{
				Description: "Polaris catalog name for this database.",
				Computed:    true,
			},
		},
	}
}

func (r *LakeDatabaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LakeDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name": plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		body["description"] = plan.Description.ValueString()
	}
	if !plan.BucketID.IsNull() && !plan.BucketID.IsUnknown() {
		body["bucketId"] = plan.BucketID.ValueString()
	}

	respBody, statusCode, err := r.client.Post("/lakehouse/catalog/databases", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating lake database", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating lake database",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	plan.ID = types.StringValue(getString(result, "id"))
	plan.S3Location = types.StringValue(getString(result, "s3Location"))
	plan.PolarisName = types.StringValue(getString(result, "polarisName"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No GET-by-id endpoint exists today. Use the LIST endpoint and filter by id.
	respBody, statusCode, err := r.client.Get("/lakehouse/catalog/databases")
	if err != nil {
		resp.Diagnostics.AddError("Error reading lake databases", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading lake databases",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var list []map[string]interface{}
	if err := json.Unmarshal(respBody, &list); err != nil {
		// Some endpoints wrap with {data: [...]} — try that shape.
		var wrapper struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err2 := json.Unmarshal(respBody, &wrapper); err2 != nil {
			resp.Diagnostics.AddError("Error parsing response", err.Error())
			return
		}
		list = wrapper.Data
	}

	id := state.ID.ValueString()
	var found map[string]interface{}
	for _, row := range list {
		if getString(row, "id") == id {
			found = row
			break
		}
	}
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(getString(found, "name"))
	if v := getString(found, "description"); v != "" {
		state.Description = types.StringValue(v)
	}
	if v := getString(found, "bucketId"); v != "" {
		state.BucketID = types.StringValue(v)
	}
	state.S3Location = types.StringValue(getString(found, "s3Location"))
	state.PolarisName = types.StringValue(getString(found, "polarisName"))

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All mutable fields are RequiresReplace; nothing to update in place.
	var plan LakeDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LakeDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/lakehouse/catalog/databases/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting lake database", err.Error())
		return
	}
	if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
		resp.Diagnostics.AddError("API error deleting lake database",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
	}
}
