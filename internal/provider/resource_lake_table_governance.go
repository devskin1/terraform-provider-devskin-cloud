package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &LakeTableGovernanceResource{}
	_ resource.ResourceWithConfigure = &LakeTableGovernanceResource{}
)

type LakeTableGovernanceResource struct {
	client *ApiClient
}

type LakeTableGovernanceResourceModel struct {
	ID         types.String `tfsdk:"id"`
	TableID    types.String `tfsdk:"table_id"`
	DatabaseID types.String `tfsdk:"database_id"`
	RowFilters types.List   `tfsdk:"row_filters"`
	ColumnMasks types.List  `tfsdk:"column_masks"`
}

type RowFilterModel struct {
	Role      types.String `tfsdk:"role"`
	Predicate types.String `tfsdk:"predicate"`
}

type ColumnMaskModel struct {
	Column     types.String `tfsdk:"column"`
	Role       types.String `tfsdk:"role"`
	MaskType   types.String `tfsdk:"mask_type"`
	Expression types.String `tfsdk:"expression"`
}

var rowFilterAttrTypes = map[string]attr.Type{
	"role":      types.StringType,
	"predicate": types.StringType,
}

var columnMaskAttrTypes = map[string]attr.Type{
	"column":     types.StringType,
	"role":       types.StringType,
	"mask_type":  types.StringType,
	"expression": types.StringType,
}

func NewLakeTableGovernanceResource() resource.Resource {
	return &LakeTableGovernanceResource{}
}

func (r *LakeTableGovernanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_table_governance"
}

func (r *LakeTableGovernanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages row-level filters and column masks for a DevskinLake table. The resource owns the full set of filters/masks for the table; create/update PUTs the entire list and Delete clears them.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Synthetic id (matches table_id).",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"table_id": schema.StringAttribute{
				Description:   "Lakehouse table id this governance applies to.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"database_id": schema.StringAttribute{
				Description: "Optional parent database id (used for refresh via list-tables fallback).",
				Optional:    true,
			},
			"row_filters": schema.ListNestedAttribute{
				Description: "Row-level filters. Each filter applies a SQL boolean predicate, optionally scoped to a role.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"role": schema.StringAttribute{
							Description: "Role this filter applies to. Empty for org-wide.",
							Optional:    true,
						},
						"predicate": schema.StringAttribute{
							Description: "SQL predicate (e.g. `tenant_id = current_user_tenant()`).",
							Required:    true,
						},
					},
				},
			},
			"column_masks": schema.ListNestedAttribute{
				Description: "Column masks. mask_type is one of: hash, redact, partial. expression is optional (used for `partial`).",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"column": schema.StringAttribute{
							Description: "Column name to mask.",
							Required:    true,
						},
						"role": schema.StringAttribute{
							Description: "Role this mask applies to. Empty for org-wide.",
							Optional:    true,
						},
						"mask_type": schema.StringAttribute{
							Description: "Mask type: hash | redact | partial.",
							Required:    true,
						},
						"expression": schema.StringAttribute{
							Description: "Optional masking expression (e.g. `concat(left(col, 2), '***')`).",
							Optional:    true,
						},
					},
				},
			},
		},
	}
}

func (r *LakeTableGovernanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeTableGovernanceResource) buildRowFilters(ctx context.Context, plan LakeTableGovernanceResourceModel, diags *diag.Diagnostics) []map[string]interface{} {
	if plan.RowFilters.IsNull() || plan.RowFilters.IsUnknown() {
		return []map[string]interface{}{}
	}
	var rfs []RowFilterModel
	d := plan.RowFilters.ElementsAs(ctx, &rfs, false)
	if d.HasError() {
		diags.Append(d...)
		return nil
	}
	out := make([]map[string]interface{}, 0, len(rfs))
	for _, rf := range rfs {
		entry := map[string]interface{}{
			"predicate": rf.Predicate.ValueString(),
		}
		if !rf.Role.IsNull() && !rf.Role.IsUnknown() && rf.Role.ValueString() != "" {
			entry["role"] = rf.Role.ValueString()
		}
		out = append(out, entry)
	}
	return out
}

func (r *LakeTableGovernanceResource) buildColumnMasks(ctx context.Context, plan LakeTableGovernanceResourceModel, diags *diag.Diagnostics) []map[string]interface{} {
	if plan.ColumnMasks.IsNull() || plan.ColumnMasks.IsUnknown() {
		return []map[string]interface{}{}
	}
	var cms []ColumnMaskModel
	d := plan.ColumnMasks.ElementsAs(ctx, &cms, false)
	if d.HasError() {
		diags.Append(d...)
		return nil
	}
	out := make([]map[string]interface{}, 0, len(cms))
	for _, cm := range cms {
		entry := map[string]interface{}{
			"column":   cm.Column.ValueString(),
			"maskType": cm.MaskType.ValueString(),
		}
		if !cm.Role.IsNull() && !cm.Role.IsUnknown() && cm.Role.ValueString() != "" {
			entry["role"] = cm.Role.ValueString()
		}
		if !cm.Expression.IsNull() && !cm.Expression.IsUnknown() && cm.Expression.ValueString() != "" {
			entry["expression"] = cm.Expression.ValueString()
		}
		out = append(out, entry)
	}
	return out
}

func (r *LakeTableGovernanceResource) putBoth(ctx context.Context, plan LakeTableGovernanceResourceModel, diags *diag.Diagnostics) bool {
	tableID := plan.TableID.ValueString()

	rowFilters := r.buildRowFilters(ctx, plan, diags)
	if diags.HasError() {
		return false
	}
	columnMasks := r.buildColumnMasks(ctx, plan, diags)
	if diags.HasError() {
		return false
	}

	rfBody := map[string]interface{}{"rowFilters": rowFilters}
	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/lakehouse/catalog/tables/%s/row-filters", tableID), rfBody)
	if err != nil {
		diags.AddError("Error putting row filters", err.Error())
		return false
	}
	if statusCode < 200 || statusCode >= 300 {
		diags.AddError("API error putting row filters",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return false
	}

	cmBody := map[string]interface{}{"columnMasks": columnMasks}
	respBody, statusCode, err = r.client.Put(fmt.Sprintf("/lakehouse/catalog/tables/%s/column-masks", tableID), cmBody)
	if err != nil {
		diags.AddError("Error putting column masks", err.Error())
		return false
	}
	if statusCode < 200 || statusCode >= 300 {
		diags.AddError("API error putting column masks",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return false
	}
	return true
}

func (r *LakeTableGovernanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LakeTableGovernanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.putBoth(ctx, plan, &resp.Diagnostics) {
		return
	}

	plan.ID = types.StringValue(plan.TableID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeTableGovernanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeTableGovernanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read uses GET /api/lakehouse/catalog/databases/:dbId/tables and finds by id.
	if state.DatabaseID.IsNull() || state.DatabaseID.IsUnknown() || state.DatabaseID.ValueString() == "" {
		// Without a database id we cannot refresh; preserve state.
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/lakehouse/catalog/databases/%s/tables", state.DatabaseID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddWarning("Error listing tables for governance refresh",
			fmt.Sprintf("err=%v — keeping existing state.", err))
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddWarning("Tables list returned non-2xx",
			fmt.Sprintf("Status %d: %s. Skipping refresh.", statusCode, string(respBody)))
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
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

	var found map[string]interface{}
	for _, row := range list {
		if getString(row, "id") == state.TableID.ValueString() {
			found = row
			break
		}
	}
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Parse rowFilters and columnMasks back into typed lists.
	if rfs, ok := found["rowFilters"].([]interface{}); ok {
		vals := make([]attr.Value, 0, len(rfs))
		for _, raw := range rfs {
			if m, ok := raw.(map[string]interface{}); ok {
				v, _ := types.ObjectValue(rowFilterAttrTypes, map[string]attr.Value{
					"role":      types.StringValue(getString(m, "role")),
					"predicate": types.StringValue(getString(m, "predicate")),
				})
				vals = append(vals, v)
			}
		}
		listVal, diags := types.ListValue(types.ObjectType{AttrTypes: rowFilterAttrTypes}, vals)
		resp.Diagnostics.Append(diags...)
		state.RowFilters = listVal
	}
	if cms, ok := found["columnMasks"].([]interface{}); ok {
		vals := make([]attr.Value, 0, len(cms))
		for _, raw := range cms {
			if m, ok := raw.(map[string]interface{}); ok {
				v, _ := types.ObjectValue(columnMaskAttrTypes, map[string]attr.Value{
					"column":     types.StringValue(getString(m, "column")),
					"role":       types.StringValue(getString(m, "role")),
					"mask_type":  types.StringValue(getString(m, "maskType")),
					"expression": types.StringValue(getString(m, "expression")),
				})
				vals = append(vals, v)
			}
		}
		listVal, diags := types.ListValue(types.ObjectType{AttrTypes: columnMaskAttrTypes}, vals)
		resp.Diagnostics.Append(diags...)
		state.ColumnMasks = listVal
	}

	state.ID = types.StringValue(state.TableID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeTableGovernanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan LakeTableGovernanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state LakeTableGovernanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !r.putBoth(ctx, plan, &resp.Diagnostics) {
		return
	}

	plan.ID = state.ID
	if plan.ID.IsNull() || plan.ID.IsUnknown() || plan.ID.ValueString() == "" {
		plan.ID = types.StringValue(plan.TableID.ValueString())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeTableGovernanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LakeTableGovernanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Clear both lists by PUTting empty arrays. Tolerate 404.
	tableID := state.TableID.ValueString()
	for _, p := range []struct {
		path string
		body map[string]interface{}
	}{
		{fmt.Sprintf("/lakehouse/catalog/tables/%s/row-filters", tableID), map[string]interface{}{"rowFilters": []interface{}{}}},
		{fmt.Sprintf("/lakehouse/catalog/tables/%s/column-masks", tableID), map[string]interface{}{"columnMasks": []interface{}{}}},
	} {
		respBody, statusCode, err := r.client.Put(p.path, p.body)
		if err != nil {
			resp.Diagnostics.AddWarning("Error clearing governance",
				fmt.Sprintf("path=%s err=%v", p.path, err))
			continue
		}
		if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
			resp.Diagnostics.AddWarning("API error clearing governance",
				fmt.Sprintf("path=%s status=%d body=%s", p.path, statusCode, string(respBody)))
		}
	}
}
