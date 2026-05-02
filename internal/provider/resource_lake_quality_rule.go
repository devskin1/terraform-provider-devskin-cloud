package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &LakeQualityRuleResource{}
	_ resource.ResourceWithConfigure = &LakeQualityRuleResource{}
)

type LakeQualityRuleResource struct {
	client *ApiClient
}

type LakeQualityRuleResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	TableID      types.String `tfsdk:"table_id"`
	Expectations types.String `tfsdk:"expectations"`
	ScheduleCron types.String `tfsdk:"schedule_cron"`
	Enabled      types.Bool   `tfsdk:"enabled"`
}

func NewLakeQualityRuleResource() resource.Resource {
	return &LakeQualityRuleResource{}
}

func (r *LakeQualityRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_quality_rule"
}

func (r *LakeQualityRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// TODO: There is no GET single endpoint for a quality rule today; Read uses the LIST endpoint.
		Description: "Manages a DevskinLake data-quality rule (Great Expectations style). `expectations` is a JSON string carrying a list of expectation objects.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "Rule name.",
				Required:    true,
			},
			"table_id": schema.StringAttribute{
				Description: "Optional id of the lakehouse table the rule applies to.",
				Optional:    true,
			},
			"expectations": schema.StringAttribute{
				Description: "JSON-encoded list of expectation objects, e.g. `jsonencode([{type=\"not_null\",column=\"id\"}])`.",
				Required:    true,
			},
			"schedule_cron": schema.StringAttribute{
				Description: "Optional cron schedule.",
				Optional:    true,
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
		},
	}
}

func (r *LakeQualityRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeQualityRuleResource) buildBody(plan LakeQualityRuleResourceModel) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"name":    plan.Name.ValueString(),
		"enabled": plan.Enabled.ValueBool(),
	}
	if !plan.TableID.IsNull() && !plan.TableID.IsUnknown() {
		body["tableId"] = plan.TableID.ValueString()
	}
	if !plan.ScheduleCron.IsNull() && !plan.ScheduleCron.IsUnknown() {
		body["scheduleCron"] = plan.ScheduleCron.ValueString()
	}

	// Parse the JSON-encoded expectations string into a slice for the API payload.
	if !plan.Expectations.IsNull() && !plan.Expectations.IsUnknown() {
		raw := plan.Expectations.ValueString()
		var parsed interface{}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, fmt.Errorf("expectations must be valid JSON: %w", err)
		}
		body["expectations"] = parsed
	}
	return body, nil
}

func (r *LakeQualityRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LakeQualityRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.buildBody(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid configuration", err.Error())
		return
	}

	respBody, statusCode, err := r.client.Post("/lakehouse/quality/rules", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating quality rule", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating quality rule",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	if id := getString(result, "id"); id != "" {
		plan.ID = types.StringValue(id)
	} else {
		plan.ID = types.StringValue(plan.Name.ValueString())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeQualityRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeQualityRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// TODO: No GET single endpoint exists today. We attempt the LIST endpoint and filter.
	respBody, statusCode, err := r.client.Get("/lakehouse/quality/rules")
	if err != nil {
		resp.Diagnostics.AddWarning("Quality rule list endpoint not reachable",
			fmt.Sprintf("err=%v — keeping existing state.", err))
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	if statusCode == 404 {
		resp.Diagnostics.AddWarning("Quality rule list endpoint missing",
			"GET /api/lakehouse/quality/rules is not implemented yet. Skipping refresh.")
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddWarning("Quality rule list returned non-2xx",
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
	if v := getString(found, "tableId"); v != "" {
		state.TableID = types.StringValue(v)
	}
	if v := getString(found, "scheduleCron"); v != "" {
		state.ScheduleCron = types.StringValue(v)
	}
	state.Enabled = types.BoolValue(getBool(found, "enabled"))
	if exp, ok := found["expectations"]; ok && exp != nil {
		if b, err := json.Marshal(exp); err == nil {
			state.Expectations = types.StringValue(string(b))
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeQualityRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan LakeQualityRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state LakeQualityRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.buildBody(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid configuration", err.Error())
		return
	}

	// TODO: PATCH endpoint may not exist yet; tolerate 404.
	respBody, statusCode, err := r.client.Patch(fmt.Sprintf("/lakehouse/quality/rules/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating quality rule", err.Error())
		return
	}
	if statusCode == 404 {
		resp.Diagnostics.AddWarning("Quality rule PATCH endpoint missing",
			"PATCH /api/lakehouse/quality/rules/:id is not implemented yet. State updated locally only.")
	} else if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating quality rule",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeQualityRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LakeQualityRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// TODO: DELETE endpoint may not exist yet; tolerate 404.
	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/lakehouse/quality/rules/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddWarning("Quality rule delete request failed",
			fmt.Sprintf("err=%v — removing from state anyway.", err))
		return
	}
	if statusCode == 404 {
		resp.Diagnostics.AddWarning("Quality rule delete endpoint missing",
			"DELETE /api/lakehouse/quality/rules/:id is not implemented yet. Removed from state.")
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting quality rule",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
	}
}
