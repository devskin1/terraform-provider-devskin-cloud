package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &FunctionResource{}
	_ resource.ResourceWithConfigure   = &FunctionResource{}
	_ resource.ResourceWithImportState = &FunctionResource{}
)

type FunctionResource struct{ client *ApiClient }

// Espelha createFunctionSchema em functions.controller.ts.
type FunctionResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Runtime     types.String `tfsdk:"runtime"`
	Handler     types.String `tfsdk:"handler"`
	Memory      types.Int64  `tfsdk:"memory"`
	Timeout     types.Int64  `tfsdk:"timeout"`
	Region      types.String `tfsdk:"region"`
	Environment types.Map    `tfsdk:"environment"`
	SourceCode  types.String `tfsdk:"source_code"`
	Status      types.String `tfsdk:"status"`
}

func NewFunctionResource() resource.Resource { return &FunctionResource{} }

func (r *FunctionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_function"
}

func (r *FunctionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Funcao serverless. Sem source_code a plataforma publica o template do runtime — " +
			"o que sobe e um placeholder, nao o seu codigo.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{
				Description: "Nome (letras, digitos e hifen). Mudar recria a funcao.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"runtime": schema.StringAttribute{
				Description: "NODEJS18, NODEJS20, PYTHON310, PYTHON311, GO1X, JAVA17 ou RUBY32.",
				Required:    true,
			},
			"handler": schema.StringAttribute{Description: "Ponto de entrada, ex: index.handler.", Required: true},
			"memory":  schema.Int64Attribute{Description: "MB, de 64 a 4096.", Optional: true, Computed: true, Default: int64default.StaticInt64(128)},
			"timeout": schema.Int64Attribute{Description: "Segundos, de 1 a 900.", Optional: true, Computed: true, Default: int64default.StaticInt64(30)},
			"region":  schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("sa-east-1")},
			"environment": schema.MapAttribute{
				Description: "Variaveis de ambiente. Use kubmix_secret para credencial — o que entra aqui fica legivel no state.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"source_code": schema.StringAttribute{
				Description: "Codigo do handler, ate 512 KB. Sem isso a funcao sobe com o template padrao do runtime.",
				Optional:    true,
			},
			"status": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *FunctionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*ApiClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *ApiClient, got something else.")
		return
	}
	r.client = client
}

func (r *FunctionResource) body(ctx context.Context, m FunctionResourceModel) map[string]interface{} {
	env := map[string]interface{}{}
	if !m.Environment.IsNull() && !m.Environment.IsUnknown() {
		var tmp map[string]string
		m.Environment.ElementsAs(ctx, &tmp, false)
		for k, v := range tmp {
			env[k] = v
		}
	}
	b := map[string]interface{}{
		"name":        m.Name.ValueString(),
		"runtime":     m.Runtime.ValueString(),
		"handler":     m.Handler.ValueString(),
		"memory":      m.Memory.ValueInt64(),
		"timeout":     m.Timeout.ValueInt64(),
		"region":      m.Region.ValueString(),
		"environment": env,
	}
	if !m.SourceCode.IsNull() && !m.SourceCode.IsUnknown() {
		b["sourceCode"] = m.SourceCode.ValueString()
	}
	return b
}

// applyFunction nao mexe em environment nem source_code: sao o que o usuario
// declarou, e a API pode devolver forma diferente (ou omitir), o que geraria
// diferenca permanente no plano.
func applyFunction(m *FunctionResourceModel, d map[string]interface{}) {
	m.ID = types.StringValue(getString(d, "id"))
	m.Name = types.StringValue(getString(d, "name"))
	m.Status = types.StringValue(getString(d, "status"))
	if v := getString(d, "runtime"); v != "" {
		m.Runtime = types.StringValue(v)
	}
	if v := getString(d, "handler"); v != "" {
		m.Handler = types.StringValue(v)
	}
	if v := getString(d, "region"); v != "" {
		m.Region = types.StringValue(v)
	}
	if v := getInt64(d, "memory"); v > 0 {
		m.Memory = types.Int64Value(v)
	}
	if v := getInt64(d, "timeout"); v > 0 {
		m.Timeout = types.Int64Value(v)
	}
}

func (r *FunctionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FunctionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Post("/functions", r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating function", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating function", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyFunction(&plan, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *FunctionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FunctionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/functions/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading function", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading function", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyFunction(&state, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *FunctionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state FunctionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	respBody, code, err := r.client.Put(fmt.Sprintf("/functions/%s", plan.ID.ValueString()), r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating function", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error updating function", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		applyFunction(&plan, unwrapData(result))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *FunctionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FunctionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(fmt.Sprintf("/functions/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting function", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting function", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}

func (r *FunctionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
