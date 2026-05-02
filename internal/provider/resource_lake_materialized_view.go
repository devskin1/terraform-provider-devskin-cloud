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
	_ resource.Resource              = &LakeMaterializedViewResource{}
	_ resource.ResourceWithConfigure = &LakeMaterializedViewResource{}
)

type LakeMaterializedViewResource struct {
	client *ApiClient
}

type LakeMaterializedViewResourceModel struct {
	ID              types.String `tfsdk:"id"`
	DatabaseID      types.String `tfsdk:"database_id"`
	Name            types.String `tfsdk:"name"`
	Query           types.String `tfsdk:"query"`
	RefreshSchedule types.String `tfsdk:"refresh_schedule"`
	LastRefreshed   types.String `tfsdk:"last_refreshed"`
	Status          types.String `tfsdk:"status"`
}

func NewLakeMaterializedViewResource() resource.Resource {
	return &LakeMaterializedViewResource{}
}

func (r *LakeMaterializedViewResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_materialized_view"
}

func (r *LakeMaterializedViewResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinLake materialized view backed by an Iceberg table. Updating `query` triggers a refresh; refresh schedule is mutable in place.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"database_id": schema.StringAttribute{
				Description:   "Parent Lakehouse database id.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Description:   "Materialized view name (must be unique within the database).",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"query": schema.StringAttribute{
				Description: "SQL SELECT statement that defines the view.",
				Required:    true,
			},
			"refresh_schedule": schema.StringAttribute{
				Description: "Optional cron / @daily / @hourly refresh schedule.",
				Optional:    true,
			},
			"last_refreshed": schema.StringAttribute{
				Description: "Timestamp of the last successful refresh.",
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Materialized view status (CREATING, READY, REFRESHING, FAILED).",
				Computed:    true,
			},
		},
	}
}

func (r *LakeMaterializedViewResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeMaterializedViewResource) buildBody(plan LakeMaterializedViewResourceModel) map[string]interface{} {
	body := map[string]interface{}{
		"name":  plan.Name.ValueString(),
		"query": plan.Query.ValueString(),
	}
	if !plan.RefreshSchedule.IsNull() && !plan.RefreshSchedule.IsUnknown() {
		body["refreshSchedule"] = plan.RefreshSchedule.ValueString()
	}
	return body
}

func (r *LakeMaterializedViewResource) applyResult(plan *LakeMaterializedViewResourceModel, result map[string]interface{}) {
	if id := getString(result, "id"); id != "" {
		plan.ID = types.StringValue(id)
	}
	if v := getString(result, "lastRefreshed"); v != "" {
		plan.LastRefreshed = types.StringValue(v)
	} else {
		plan.LastRefreshed = types.StringValue("")
	}
	if v := getString(result, "status"); v != "" {
		plan.Status = types.StringValue(v)
	} else {
		plan.Status = types.StringValue("")
	}
}

func (r *LakeMaterializedViewResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LakeMaterializedViewResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Post(
		fmt.Sprintf("/lakehouse/catalog/databases/%s/materialized-views", plan.DatabaseID.ValueString()),
		r.buildBody(plan),
	)
	if err != nil {
		resp.Diagnostics.AddError("Error creating materialized view", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating materialized view",
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
	r.applyResult(&plan, result)
	if plan.ID.IsNull() || plan.ID.IsUnknown() || plan.ID.ValueString() == "" {
		// Fall back so state has a non-null id for the next Read.
		plan.ID = types.StringValue(plan.Name.ValueString())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeMaterializedViewResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeMaterializedViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Try GET single first; fall back to LIST + filter if 404.
	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/lakehouse/catalog/materialized-views/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading materialized view", err.Error())
		return
	}

	if statusCode == 404 {
		// LIST + filter fallback.
		listBody, listStatus, listErr := r.client.Get(fmt.Sprintf("/lakehouse/catalog/databases/%s/materialized-views", state.DatabaseID.ValueString()))
		if listErr != nil {
			resp.Diagnostics.AddError("Error listing materialized views", listErr.Error())
			return
		}
		if listStatus == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		if listStatus < 200 || listStatus >= 300 {
			resp.Diagnostics.AddError("API error listing materialized views",
				fmt.Sprintf("Status %d: %s", listStatus, string(listBody)))
			return
		}
		var list []map[string]interface{}
		if err := json.Unmarshal(listBody, &list); err != nil {
			var wrapper struct {
				Data []map[string]interface{} `json:"data"`
			}
			if err2 := json.Unmarshal(listBody, &wrapper); err2 != nil {
				resp.Diagnostics.AddError("Error parsing response", err.Error())
				return
			}
			list = wrapper.Data
		}
		var found map[string]interface{}
		for _, row := range list {
			if getString(row, "id") == state.ID.ValueString() {
				found = row
				break
			}
		}
		if found == nil {
			resp.State.RemoveResource(ctx)
			return
		}
		state.Name = types.StringValue(getString(found, "name"))
		if v := getString(found, "query"); v != "" {
			state.Query = types.StringValue(v)
		}
		if v := getString(found, "refreshSchedule"); v != "" {
			state.RefreshSchedule = types.StringValue(v)
		}
		state.LastRefreshed = types.StringValue(getString(found, "lastRefreshed"))
		state.Status = types.StringValue(getString(found, "status"))
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}

	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading materialized view",
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

	state.Name = types.StringValue(getString(result, "name"))
	if v := getString(result, "query"); v != "" {
		state.Query = types.StringValue(v)
	}
	if v := getString(result, "refreshSchedule"); v != "" {
		state.RefreshSchedule = types.StringValue(v)
	}
	state.LastRefreshed = types.StringValue(getString(result, "lastRefreshed"))
	state.Status = types.StringValue(getString(result, "status"))

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeMaterializedViewResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan LakeMaterializedViewResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state LakeMaterializedViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If the query changed, call the refresh endpoint to rematerialize.
	if !plan.Query.Equal(state.Query) {
		respBody, statusCode, err := r.client.Post(
			fmt.Sprintf("/lakehouse/catalog/materialized-views/%s/refresh", state.ID.ValueString()),
			map[string]interface{}{
				"query": plan.Query.ValueString(),
			},
		)
		if err != nil {
			resp.Diagnostics.AddError("Error refreshing materialized view", err.Error())
			return
		}
		if statusCode == 404 {
			resp.Diagnostics.AddWarning("Materialized view refresh endpoint missing",
				"POST /api/lakehouse/catalog/materialized-views/:id/refresh is not implemented yet. State updated locally only.")
		} else if statusCode < 200 || statusCode >= 300 {
			resp.Diagnostics.AddError("API error refreshing materialized view",
				fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
			return
		} else {
			// Best-effort parse to refresh computed fields.
			var result map[string]interface{}
			if err := json.Unmarshal(respBody, &result); err == nil {
				if data, ok := result["data"].(map[string]interface{}); ok {
					result = data
				}
				if v := getString(result, "lastRefreshed"); v != "" {
					plan.LastRefreshed = types.StringValue(v)
				}
				if v := getString(result, "status"); v != "" {
					plan.Status = types.StringValue(v)
				}
			}
		}
	}

	plan.ID = state.ID
	if plan.LastRefreshed.IsNull() || plan.LastRefreshed.IsUnknown() {
		plan.LastRefreshed = state.LastRefreshed
	}
	if plan.Status.IsNull() || plan.Status.IsUnknown() {
		plan.Status = state.Status
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeMaterializedViewResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LakeMaterializedViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/lakehouse/catalog/materialized-views/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting materialized view", err.Error())
		return
	}
	if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
		resp.Diagnostics.AddError("API error deleting materialized view",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
	}
}
