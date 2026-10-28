package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &IamUserResource{}
	_ resource.ResourceWithConfigure   = &IamUserResource{}
	_ resource.ResourceWithImportState = &IamUserResource{}
)

type IamUserResource struct{ client *ApiClient }

// Espelha createIamUserSchema/updateIamUserSchema em iam.controller.ts.
type IamUserResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Username    types.String `tfsdk:"username"`
	DisplayName types.String `tfsdk:"display_name"`
	Email       types.String `tfsdk:"email"`
	Kind        types.String `tfsdk:"kind"`
	Policies    types.List   `tfsdk:"policies"`
	Status      types.String `tfsdk:"status"`
}

func NewIamUserResource() resource.Resource { return &IamUserResource{} }

func (r *IamUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_user"
}

func (r *IamUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Identidade IAM da organizacao. Use kind=\"programmatic\" para servico (CI, script, app) e " +
			"pendure a credencial num kubmix_access_key. Para pod no Kubernetes prefira kubmix_iam_role com " +
			"irsa_trust: o pod assume a role e nao existe chave estatica para vazar.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"username": schema.StringAttribute{
				Description: "Unico na organizacao. Trocar recria o usuario, porque a API nao renomeia.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{Description: "Nome legivel.", Required: true},
			"email": schema.StringAttribute{
				Description: "Obrigatorio quando kind=\"human\"; ignorado em \"programmatic\".",
				Optional:    true,
			},
			"kind": schema.StringAttribute{
				Description: "\"programmatic\" (sem login no painel) ou \"human\". Trocar recria.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("programmatic"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"policies": schema.ListAttribute{
				Description: "Policies gerenciadas anexadas, por nome (ex.: [\"StorageFullAccess\"]). " +
					"Sem policy o usuario autentica e nao consegue fazer nada.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"status": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *IamUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func policiesDoPlano(ctx context.Context, l types.List) []string {
	out := []string{}
	if l.IsNull() || l.IsUnknown() {
		return out
	}
	var vs []types.String
	l.ElementsAs(ctx, &vs, false)
	for _, v := range vs {
		if s := v.ValueString(); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func applyIamUser(ctx context.Context, m *IamUserResourceModel, d map[string]interface{}) {
	m.ID = types.StringValue(getString(d, "id"))
	m.Username = types.StringValue(getString(d, "username"))
	m.DisplayName = types.StringValue(getString(d, "displayName"))
	m.Status = types.StringValue(getString(d, "status"))
	if v := getString(d, "email"); v != "" {
		m.Email = types.StringValue(v)
	}
	if v := getString(d, "kind"); v != "" {
		m.Kind = types.StringValue(v)
	}
	// `policies` volta como lista de strings OU de objetos, conforme quem gravou.
	// So sobrescreve o estado quando da para ler nomes; caso contrario mantem o
	// que o plano pediu, senao todo refresh inventaria um diff.
	if raw, ok := d["policies"].([]interface{}); ok {
		nomes := []string{}
		for _, it := range raw {
			switch v := it.(type) {
			case string:
				nomes = append(nomes, v)
			case map[string]interface{}:
				if n := getString(v, "name"); n != "" {
					nomes = append(nomes, n)
				}
			}
		}
		if len(nomes) > 0 || len(raw) == 0 {
			lista, diags := types.ListValueFrom(ctx, types.StringType, nomes)
			if !diags.HasError() {
				m.Policies = lista
			}
		}
	}
}

func (r *IamUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan IamUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]interface{}{
		"username":    plan.Username.ValueString(),
		"displayName": plan.DisplayName.ValueString(),
		"kind":        plan.Kind.ValueString(),
		"policies":    policiesDoPlano(ctx, plan.Policies),
		// A API cria uma chave inicial por padrao e devolve o segredo UMA vez.
		// Aqui isso seria um segredo que ninguem captura: quem quer credencial
		// declara um kubmix_access_key, que guarda o segredo no state.
		"createInitialAccessKey": false,
	}
	if !plan.Email.IsNull() && plan.Email.ValueString() != "" {
		body["email"] = plan.Email.ValueString()
	}

	respBody, code, err := r.client.Post("/iam/users", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating IAM user", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating IAM user", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	d := unwrapData(result)
	applyIamUser(ctx, &plan, d)

	// A API IGNORA `createInitialAccessKey` e cria uma chave sempre (medido em
	// 28/10: false e true produzem 1 chave). Essa chave nasce fora do controle
	// do Terraform: ocupa uma das 2 vagas do usuario, e o segredo dela so
	// apareceu nesta resposta — ninguem declarou, ninguem guarda. Deixa-la viva
	// faria um unico kubmix_access_key ja estourar o limite, e deixaria uma
	// credencial valida solta que o destroy nao alcanca.
	//
	// Entao apaga: o usuario nasce com ZERO chaves e quem manda no ciclo de
	// vida delas e o kubmix_access_key. Import nao passa por aqui, entao chave
	// de usuario pre-existente nao e tocada.
	if ids := chavesIniciais(d); len(ids) > 0 {
		for _, kid := range ids {
			_, code, derr := r.client.Delete(fmt.Sprintf("/iam/users/%s/access-keys/%s", plan.ID.ValueString(), kid))
			if derr != nil || (code < 200 || code >= 300) && code != 404 {
				resp.Diagnostics.AddWarning("Chave inicial ficou para tras",
					fmt.Sprintf("A API criou a chave %s sozinha e nao consegui remove-la (%v / status %d). "+
						"Ela ocupa uma das 2 vagas do usuario; apague pelo painel.", kid, derr, code))
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// chavesIniciais le as chaves que vieram na resposta da criacao. O registro da
// chave inicial nao tem campo `id` — a listagem cai no accessKeyId como id, e a
// rota de delete aceita esse valor.
func chavesIniciais(d map[string]interface{}) []string {
	raw, ok := d["accessKeys"].([]interface{})
	if !ok {
		return nil
	}
	out := []string{}
	for _, it := range raw {
		k, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		if id := getString(k, "id"); id != "" {
			out = append(out, id)
		} else if akid := getString(k, "accessKeyId"); akid != "" {
			out = append(out, akid)
		}
	}
	return out
}

func (r *IamUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state IamUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/iam/users/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading IAM user", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading IAM user", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	d := unwrapData(result)
	if recursoFoiDestruido(getString(d, "status")) {
		resp.State.RemoveResource(ctx)
		return
	}
	applyIamUser(ctx, &state, d)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *IamUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state IamUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	// PATCH, nao PUT: a rota de update e PATCH e so aceita os campos mutaveis.
	// `accessKeys` fica de fora de proposito — quem mexe nelas e o
	// kubmix_access_key; mandar o campo aqui apagaria chaves vivas.
	body := map[string]interface{}{
		"displayName": plan.DisplayName.ValueString(),
		"policies":    policiesDoPlano(ctx, plan.Policies),
	}
	if !plan.Email.IsNull() && plan.Email.ValueString() != "" {
		body["email"] = plan.Email.ValueString()
	}
	respBody, code, err := r.client.Patch(fmt.Sprintf("/iam/users/%s", plan.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating IAM user", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error updating IAM user", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		applyIamUser(ctx, &plan, unwrapData(result))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *IamUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state IamUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(fmt.Sprintf("/iam/users/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting IAM user", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting IAM user", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}

func (r *IamUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
