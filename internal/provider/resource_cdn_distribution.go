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
	_ resource.Resource                = &CDNDistributionResource{}
	_ resource.ResourceWithConfigure   = &CDNDistributionResource{}
	_ resource.ResourceWithImportState = &CDNDistributionResource{}
)

type CDNDistributionResource struct{ client *ApiClient }

// Espelha create/updateDistributionSchema em cdn.controller.ts.
type CDNDistributionResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	OriginDomain types.String `tfsdk:"origin_domain"`
	Aliases      types.List   `tfsdk:"aliases"`
	CustomDomain types.String `tfsdk:"custom_domain"`
	WAFID        types.String `tfsdk:"waf_id"`
	SSLMode      types.String `tfsdk:"ssl_mode"`
	ForceHTTPS   types.Bool   `tfsdk:"force_https"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	Status       types.String `tfsdk:"status"`
	DomainName   types.String `tfsdk:"domain_name"`
}

func NewCDNDistributionResource() resource.Resource { return &CDNDistributionResource{} }

func (r *CDNDistributionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdn_distribution"
}

func (r *CDNDistributionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Distribuicao de CDN na borda, com TLS e WAF opcional.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Description: "Nome (letras, digitos e hifen).", Required: true},
			"origin_domain": schema.StringAttribute{
				Description: "Dominio de origem que a CDN busca (ex: o endpoint do bucket ou do balanceador).",
				Required:    true,
			},
			"aliases": schema.ListAttribute{
				Description: "Dominios alternativos servidos por esta distribuicao.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"custom_domain": schema.StringAttribute{Description: "Dominio proprio do cliente.", Optional: true},
			"waf_id":        schema.StringAttribute{Description: "Regra de WAF aplicada na borda.", Optional: true},
			"ssl_mode": schema.StringAttribute{
				Description: "flexible = HTTPS na borda e HTTP na origem (origem nao precisa de certificado). " +
					"full = HTTPS ate a origem.",
				Optional: true, Computed: true, Default: stringdefault.StaticString("flexible"),
			},
			"force_https": schema.BoolAttribute{
				Description: "true faz a porta 80 responder 301 para HTTPS. false serve o conteudo em HTTP tambem.",
				Optional:    true, Computed: true, Default: booldefault.StaticBool(true),
			},
			"enabled":     schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"status":      schema.StringAttribute{Computed: true},
			"domain_name": schema.StringAttribute{Description: "Dominio publicado pela plataforma.", Computed: true},
		},
	}
}

func (r *CDNDistributionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *CDNDistributionResource) body(ctx context.Context, m CDNDistributionResourceModel) map[string]interface{} {
	aliases := []string{}
	if !m.Aliases.IsNull() && !m.Aliases.IsUnknown() {
		m.Aliases.ElementsAs(ctx, &aliases, false)
	}
	// A API recebe origins como lista livre (z.array(z.any())), entao NAO
	// valida o formato de dentro — o campo errado passa e so aparece na hora
	// de servir. O proxy da CDN le `origins[0].domain`; mandavamos
	// `domainName` e o resultado era distribuicao criada com sucesso servindo
	// 502 "No origin configured". `path` acompanha porque o proxy tambem o le.
	b := map[string]interface{}{
		"name":       m.Name.ValueString(),
		"origins":    []map[string]interface{}{{"domain": m.OriginDomain.ValueString(), "path": "/"}},
		"aliases":    aliases,
		"sslMode":    m.SSLMode.ValueString(),
		"forceHttps": m.ForceHTTPS.ValueBool(),
		"enabled":    m.Enabled.ValueBool(),
	}
	if !m.CustomDomain.IsNull() && !m.CustomDomain.IsUnknown() {
		b["customDomain"] = m.CustomDomain.ValueString()
	}
	if !m.WAFID.IsNull() && !m.WAFID.IsUnknown() {
		b["wafId"] = m.WAFID.ValueString()
	}
	return b
}

func applyCDN(m *CDNDistributionResourceModel, d map[string]interface{}) {
	m.ID = types.StringValue(getString(d, "id"))
	m.Name = types.StringValue(getString(d, "name"))
	m.Status = types.StringValue(getString(d, "status"))
	m.DomainName = types.StringValue(getString(d, "domainName"))
	m.Enabled = types.BoolValue(getBool(d, "enabled"))
	// sslMode e forceHttps NAO vem no topo da resposta: o backend os funde
	// dentro de "tags" (mergedTags em cdn.controller.ts). Ler do topo devolvia
	// vazio e sobrescrevia o valor planejado por false — o que fazia o
	// Terraform abortar com "provider produced inconsistent result after
	// apply". Lemos de tags e, se nao houver, preservamos o que foi planejado.
	if tags, ok := d["tags"].(map[string]interface{}); ok {
		if v := getString(tags, "sslMode"); v != "" {
			m.SSLMode = types.StringValue(v)
		}
		if _, existe := tags["forceHttps"]; existe {
			m.ForceHTTPS = types.BoolValue(getBool(tags, "forceHttps"))
		}
	}
	// Le a origem de volta. Sem isto o `plan` nunca acusa desvio (alguem muda
	// a origem pela API e o Terraform nao ve) e o `import` traz origin_domain
	// vazio, o que gera um replace desnecessario no primeiro apply.
	// O backend guarda origins ora como array, ora como string JSON — depende
	// de ter passado pelo Prisma ou pela serializacao do controller.
	if dom := origemDoResultado(d["origins"]); dom != "" {
		m.OriginDomain = types.StringValue(dom)
	}
}

// origemDoResultado extrai origins[0].domain aceitando array ou string JSON.
func origemDoResultado(v interface{}) string {
	var lista []interface{}
	switch t := v.(type) {
	case []interface{}:
		lista = t
	case string:
		if t == "" {
			return ""
		}
		if err := json.Unmarshal([]byte(t), &lista); err != nil {
			return ""
		}
	default:
		return ""
	}
	if len(lista) == 0 {
		return ""
	}
	primeiro, ok := lista[0].(map[string]interface{})
	if !ok {
		return ""
	}
	return getString(primeiro, "domain")
}

func (r *CDNDistributionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CDNDistributionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Post("/cdn/distributions", r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating cdn distribution", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating cdn distribution", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyCDN(&plan, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CDNDistributionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CDNDistributionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/cdn/distributions/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading cdn distribution", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading cdn distribution", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyCDN(&state, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *CDNDistributionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CDNDistributionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	respBody, code, err := r.client.Patch(fmt.Sprintf("/cdn/distributions/%s", plan.ID.ValueString()), r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating cdn distribution", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error updating cdn distribution", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		applyCDN(&plan, unwrapData(result))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CDNDistributionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CDNDistributionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(fmt.Sprintf("/cdn/distributions/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting cdn distribution", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting cdn distribution", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}

func (r *CDNDistributionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
