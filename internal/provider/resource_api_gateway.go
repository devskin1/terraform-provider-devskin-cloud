package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ApiGatewayResource{}
	_ resource.ResourceWithConfigure   = &ApiGatewayResource{}
	_ resource.ResourceWithImportState = &ApiGatewayResource{}
)

type ApiGatewayResource struct{ client *ApiClient }

// Espelha createApiGatewaySchema em apigateway.controller.ts.
type ApiGatewayResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Protocol          types.String `tfsdk:"protocol"`
	AuthType          types.String `tfsdk:"auth_type"`
	CorsEnabled       types.Bool   `tfsdk:"cors_enabled"`
	CorsOrigins       types.List   `tfsdk:"cors_origins"`
	ThrottlingEnabled types.Bool   `tfsdk:"throttling_enabled"`
	BurstLimit        types.Int64  `tfsdk:"burst_limit"`
	RateLimit         types.Int64  `tfsdk:"rate_limit"`
	Status            types.String `tfsdk:"status"`
}

func NewApiGatewayResource() resource.Resource { return &ApiGatewayResource{} }

func (r *ApiGatewayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_gateway"
}

func (r *ApiGatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "API Gateway: porta de entrada das funcoes, com CORS emitido num lugar so e limite de requisicao. " +
			"Emitir CORS aqui E na aplicacao gera cabecalho duplicado, que o browser recusa — escolha um dos dois.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Description: "Nome, unico dentro da organizacao.", Required: true},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"protocol":    schema.StringAttribute{Description: "REST ou HTTP.", Optional: true, Computed: true, Default: stringdefault.StaticString("REST")},
			"auth_type":   schema.StringAttribute{Description: "Tipo de autorizacao na borda.", Optional: true, Computed: true, Default: stringdefault.StaticString("NONE")},
			"cors_enabled": schema.BoolAttribute{
				Description: "Emite os cabecalhos de CORS na borda.",
				Optional:    true, Computed: true, Default: booldefault.StaticBool(false),
			},
			"cors_origins": schema.ListAttribute{
				Description: "Origens permitidas. Evite \"*\" quando houver credencial na requisicao.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"throttling_enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"burst_limit":        schema.Int64Attribute{Description: "Pico instantaneo de requisicoes.", Optional: true},
			"rate_limit":         schema.Int64Attribute{Description: "Requisicoes por segundo em regime.", Optional: true},
			"status":             schema.StringAttribute{Computed: true},
		},
	}
}

func (r *ApiGatewayResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ApiGatewayResource) body(ctx context.Context, m ApiGatewayResourceModel) map[string]interface{} {
	origins := []string{}
	if !m.CorsOrigins.IsNull() && !m.CorsOrigins.IsUnknown() {
		m.CorsOrigins.ElementsAs(ctx, &origins, false)
	}
	b := map[string]interface{}{
		"name":              m.Name.ValueString(),
		"description":       m.Description.ValueString(),
		"protocol":          m.Protocol.ValueString(),
		"authType":          m.AuthType.ValueString(),
		"corsEnabled":       m.CorsEnabled.ValueBool(),
		"corsOrigins":       origins,
		"throttlingEnabled": m.ThrottlingEnabled.ValueBool(),
	}
	if !m.BurstLimit.IsNull() && !m.BurstLimit.IsUnknown() {
		b["burstLimit"] = m.BurstLimit.ValueInt64()
	}
	if !m.RateLimit.IsNull() && !m.RateLimit.IsUnknown() {
		b["rateLimit"] = m.RateLimit.ValueInt64()
	}
	return b
}

func applyApiGateway(m *ApiGatewayResourceModel, d map[string]interface{}) {
	m.ID = types.StringValue(getString(d, "id"))
	m.Name = types.StringValue(getString(d, "name"))
	m.Status = types.StringValue(getString(d, "status"))
	m.Description = types.StringValue(getString(d, "description"))
	if v := getString(d, "protocol"); v != "" {
		m.Protocol = types.StringValue(v)
	}
	if v := getString(d, "authType"); v != "" {
		m.AuthType = types.StringValue(v)
	}
	m.CorsEnabled = types.BoolValue(getBool(d, "corsEnabled"))
	m.ThrottlingEnabled = types.BoolValue(getBool(d, "throttlingEnabled"))
}

func (r *ApiGatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApiGatewayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Post("/api-gateway/apis", r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating api gateway", err.Error())
		return
	}
	if code == 409 {
		resp.Diagnostics.AddError("Nome de API Gateway ja em uso",
			fmt.Sprintf("Ja existe um gateway chamado %q nesta organizacao.", plan.Name.ValueString()))
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating api gateway", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyApiGateway(&plan, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApiGatewayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApiGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/api-gateway/apis/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading api gateway", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading api gateway", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyApiGateway(&state, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ApiGatewayResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ApiGatewayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	respBody, code, err := r.client.Patch(fmt.Sprintf("/api-gateway/apis/%s", plan.ID.ValueString()), r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating api gateway", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error updating api gateway", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		applyApiGateway(&plan, unwrapData(result))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ApiGatewayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApiGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(fmt.Sprintf("/api-gateway/apis/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting api gateway", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting api gateway", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}

func (r *ApiGatewayResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
