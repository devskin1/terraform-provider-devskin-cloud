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
	_ resource.Resource              = &LakeSavedQueryResource{}
	_ resource.ResourceWithConfigure = &LakeSavedQueryResource{}
)

type LakeSavedQueryResource struct {
	client *ApiClient
}

type LakeSavedQueryResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Query        types.String `tfsdk:"query"`
	ScheduleCron types.String `tfsdk:"schedule_cron"`
	LastRun      types.String `tfsdk:"last_run"`
}

func NewLakeSavedQueryResource() resource.Resource {
	return &LakeSavedQueryResource{}
}

func (r *LakeSavedQueryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_saved_query"
}

func (r *LakeSavedQueryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinLake saved SQL query (with optional cron schedule).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "Saved-query name.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Optional description.",
				Optional:    true,
			},
			"query": schema.StringAttribute{
				Description: "SQL statement to save.",
				Required:    true,
			},
			"schedule_cron": schema.StringAttribute{
				Description: "Optional cron expression for scheduled execution.",
				Optional:    true,
			},
			"last_run": schema.StringAttribute{
				Description: "Timestamp of the most recent execution.",
				Computed:    true,
			},
		},
	}
}

func (r *LakeSavedQueryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeSavedQueryResource) buildBody(plan LakeSavedQueryResourceModel) map[string]interface{} {
	body := map[string]interface{}{
		"name":  plan.Name.ValueString(),
		"query": plan.Query.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		body["description"] = plan.Description.ValueString()
	}
	if !plan.ScheduleCron.IsNull() && !plan.ScheduleCron.IsUnknown() {
		body["scheduleCron"] = plan.ScheduleCron.ValueString()
	}
	return body
}

func (r *LakeSavedQueryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LakeSavedQueryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Post("/lakehouse/sql/saved", r.buildBody(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating saved query", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating saved query",
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
	if id := getString(result, "id"); id != "" {
		plan.ID = types.StringValue(id)
	} else {
		plan.ID = types.StringValue(plan.Name.ValueString())
	}
	plan.LastRun = types.StringValue(getString(result, "lastRunAt"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeSavedQueryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeSavedQueryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/lakehouse/sql/saved/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading saved query", err.Error())
		return
	}

	if statusCode == 404 {
		// Fall back to LIST + filter.
		listBody, listStatus, listErr := r.client.Get("/lakehouse/sql/saved")
		if listErr != nil {
			resp.Diagnostics.AddError("Error listing saved queries", listErr.Error())
			return
		}
		if listStatus < 200 || listStatus >= 300 {
			resp.State.RemoveResource(ctx)
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
		r.applyMap(&state, found)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}

	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading saved query",
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
	r.applyMap(&state, result)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeSavedQueryResource) applyMap(state *LakeSavedQueryResourceModel, m map[string]interface{}) {
	state.Name = types.StringValue(getString(m, "name"))
	if v := getString(m, "description"); v != "" {
		state.Description = types.StringValue(v)
	}
	if v := getString(m, "query"); v != "" {
		state.Query = types.StringValue(v)
	}
	if v := getString(m, "scheduleCron"); v != "" {
		state.ScheduleCron = types.StringValue(v)
	}
	state.LastRun = types.StringValue(getString(m, "lastRunAt"))
}

func (r *LakeSavedQueryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan LakeSavedQueryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state LakeSavedQueryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Patch(fmt.Sprintf("/lakehouse/sql/saved/%s", state.ID.ValueString()), r.buildBody(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating saved query", err.Error())
		return
	}
	if statusCode == 404 {
		resp.Diagnostics.AddWarning("Saved query PATCH endpoint missing",
			"PATCH /api/lakehouse/sql/saved/:id is not implemented yet. State updated locally only.")
	} else if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating saved query",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	plan.ID = state.ID
	if plan.LastRun.IsNull() || plan.LastRun.IsUnknown() {
		plan.LastRun = state.LastRun
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeSavedQueryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LakeSavedQueryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/lakehouse/sql/saved/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting saved query", err.Error())
		return
	}
	if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
		resp.Diagnostics.AddError("API error deleting saved query",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
	}
}
