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
	_ resource.Resource                = &SecretResource{}
	_ resource.ResourceWithConfigure   = &SecretResource{}
	_ resource.ResourceWithImportState = &SecretResource{}
)

type SecretResource struct{ client *ApiClient }

// Espelha create/updateSecretSchema em secrets.controller.ts.
type SecretResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	Type             types.String `tfsdk:"type"`
	SecretValue      types.String `tfsdk:"secret_value"`
	RotationEnabled  types.Bool   `tfsdk:"rotation_enabled"`
	RotationInterval types.Int64  `tfsdk:"rotation_interval"`
	Status           types.String `tfsdk:"status"`
}

func NewSecretResource() resource.Resource { return &SecretResource{} }

func (r *SecretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *SecretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Segredo no cofre da plataforma (credencial, chave, token), com rotacao opcional.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{
				Description: "Nome do segredo.",
				Required:    true,
			},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"type":        schema.StringAttribute{Description: "Rotulo do tipo (padrao Credentials).", Optional: true, Computed: true, Default: stringdefault.StaticString("Credentials")},
			"secret_value": schema.StringAttribute{
				Description: "Conteudo do segredo. Marcado como Sensitive: nao aparece no plano nem no log do Terraform. " +
					"Ainda assim ele fica em claro no arquivo de state — proteja o backend do state.",
				Optional:  true,
				Sensitive: true,
			},
			"rotation_enabled":  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"rotation_interval": schema.Int64Attribute{Description: "Dias entre rotacoes. So vale com rotation_enabled.", Optional: true},
			"status":            schema.StringAttribute{Computed: true},
		},
	}
}

func (r *SecretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SecretResource) body(m SecretResourceModel) map[string]interface{} {
	b := map[string]interface{}{
		"name":            m.Name.ValueString(),
		"description":     m.Description.ValueString(),
		"type":            m.Type.ValueString(),
		"rotationEnabled": m.RotationEnabled.ValueBool(),
	}
	if !m.SecretValue.IsNull() && !m.SecretValue.IsUnknown() {
		b["secretValue"] = m.SecretValue.ValueString()
	}
	// null explicito quando nao definido: o schema aceita nullable e o backend
	// guarda null. Mandar 0 ligaria uma rotacao diaria sem ninguem pedir.
	if !m.RotationInterval.IsNull() && !m.RotationInterval.IsUnknown() {
		b["rotationInterval"] = m.RotationInterval.ValueInt64()
	} else {
		b["rotationInterval"] = nil
	}
	return b
}

// applySecret NAO sobrescreve secret_value: a API nao devolve o valor em claro
// na leitura, entao copiar de volta apagaria o que esta no state.
func applySecret(m *SecretResourceModel, d map[string]interface{}) {
	m.ID = types.StringValue(getString(d, "id"))
	m.Name = types.StringValue(getString(d, "name"))
	m.Status = types.StringValue(getString(d, "status"))
	m.Description = types.StringValue(getString(d, "description"))
	if v := getString(d, "type"); v != "" {
		m.Type = types.StringValue(v)
	}
	m.RotationEnabled = types.BoolValue(getBool(d, "rotationEnabled"))
}

func (r *SecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Post("/secrets", r.body(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating secret", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating secret", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applySecret(&plan, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/secrets/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading secret", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading secret", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applySecret(&state, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *SecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	respBody, code, err := r.client.Put(fmt.Sprintf("/secrets/%s", plan.ID.ValueString()), r.body(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating secret", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error updating secret", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		applySecret(&plan, unwrapData(result))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(fmt.Sprintf("/secrets/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting secret", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting secret", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}

func (r *SecretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
