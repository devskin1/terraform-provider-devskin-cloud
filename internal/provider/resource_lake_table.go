package provider

// devskin_lake_table — manages an Iceberg table inside a DevskinLake catalog
// database. The backend has POST + DELETE but no PATCH for table schema, so
// every attribute is RequiresReplace; Update is a state-only no-op.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &LakeTableResource{}
	_ resource.ResourceWithConfigure = &LakeTableResource{}
)

type LakeTableResource struct {
	client *ApiClient
}

type LakeTableColumnModel struct {
	Name types.String `tfsdk:"name"`
	Type types.String `tfsdk:"type"`
}

type LakeTableResourceModel struct {
	ID         types.String `tfsdk:"id"`
	DatabaseID types.String `tfsdk:"database_id"`
	Name       types.String `tfsdk:"name"`
	Columns    types.List   `tfsdk:"columns"`
	S3Location types.String `tfsdk:"s3_location"`
	Format     types.String `tfsdk:"format"`
}

var lakeTableColumnAttrTypes = map[string]attr.Type{
	"name": types.StringType,
	"type": types.StringType,
}

func NewLakeTableResource() resource.Resource {
	return &LakeTableResource{}
}

func (r *LakeTableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_table"
}

func (r *LakeTableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Iceberg table inside a DevskinLake catalog database. Schema cannot be patched in place — any change to columns triggers a destroy/replace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"database_id": schema.StringAttribute{
				Description:   "ID of the DevskinLake catalog database that owns this table.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Description:   "Table name (lowercase, no spaces).",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"columns": schema.ListNestedAttribute{
				Description: "Column definitions in declaration order. Cannot be modified after creation — changing this list forces a destroy/replace.",
				Required:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "Column name.",
							Required:    true,
						},
						"type": schema.StringAttribute{
							Description: "Iceberg/Trino type, e.g. bigint, double, timestamp, varchar.",
							Required:    true,
						},
					},
				},
			},
			"s3_location": schema.StringAttribute{
				Description: "S3 URI prefix backing the table.",
				Computed:    true,
			},
			"format": schema.StringAttribute{
				Description: "Storage format reported by the catalog (typically `iceberg`).",
				Computed:    true,
			},
		},
	}
}

func (r *LakeTableResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeTableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LakeTableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var cols []LakeTableColumnModel
	resp.Diagnostics.Append(plan.Columns.ElementsAs(ctx, &cols, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	colsBody := make([]map[string]string, 0, len(cols))
	for _, c := range cols {
		colsBody = append(colsBody, map[string]string{
			"name": c.Name.ValueString(),
			"type": c.Type.ValueString(),
		})
	}

	body := map[string]interface{}{
		"name":    plan.Name.ValueString(),
		"columns": colsBody,
	}

	respBody, statusCode, err := r.client.Post(
		fmt.Sprintf("/lakehouse/catalog/databases/%s/tables", plan.DatabaseID.ValueString()),
		body,
	)
	if err != nil {
		resp.Diagnostics.AddError("Error creating lake table", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating lake table",
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
	plan.Format = types.StringValue(getString(result, "format"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeTableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeTableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No GET-by-id endpoint for tables. List them inside the parent database
	// and locate by id (fallback to name if id is missing in old state).
	respBody, statusCode, err := r.client.Get(
		fmt.Sprintf("/lakehouse/catalog/databases/%s/tables", state.DatabaseID.ValueString()),
	)
	if err != nil {
		resp.Diagnostics.AddError("Error listing lake tables", err.Error())
		return
	}
	if statusCode == 404 {
		// Database is gone — table can't exist either.
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error listing lake tables",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var list []map[string]interface{}
	if err := json.Unmarshal(respBody, &list); err != nil {
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
	name := state.Name.ValueString()
	var found map[string]interface{}
	for _, row := range list {
		if (id != "" && getString(row, "id") == id) || (id == "" && name != "" && getString(row, "name") == name) {
			found = row
			break
		}
	}
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if v := getString(found, "id"); v != "" {
		state.ID = types.StringValue(v)
	}
	if v := getString(found, "name"); v != "" {
		state.Name = types.StringValue(v)
	}
	state.S3Location = types.StringValue(getString(found, "s3Location"))
	state.Format = types.StringValue(getString(found, "format"))

	// columns is RequiresReplace so we don't refresh it from the API — leave
	// whatever the prior plan/config recorded so terraform plan stays clean.

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeTableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All schema fields are RequiresReplace; nothing meaningful to update in
	// place. Pass the plan through to keep state consistent.
	var plan LakeTableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeTableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LakeTableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, statusCode, err := r.client.Delete(
		fmt.Sprintf("/lakehouse/catalog/tables/%s", state.ID.ValueString()),
	)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting lake table", err.Error())
		return
	}
	if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
		resp.Diagnostics.AddError("API error deleting lake table",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
	}
}

// Reference the attr-types map so go vet doesn't flag it as unused. It exists
// for any future code that needs to build a types.List of column objects from
// scratch (e.g. reading columns back from the API once the catalog returns
// them).
var _ = lakeTableColumnAttrTypes
