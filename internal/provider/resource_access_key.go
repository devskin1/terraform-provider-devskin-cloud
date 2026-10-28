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
	_ resource.Resource              = &AccessKeyResource{}
	_ resource.ResourceWithConfigure = &AccessKeyResource{}
)

type AccessKeyResource struct{ client *ApiClient }

type AccessKeyResourceModel struct {
	ID              types.String `tfsdk:"id"`
	IamUserID       types.String `tfsdk:"iam_user_id"`
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	Status          types.String `tfsdk:"status"`
	CreatedAt       types.String `tfsdk:"created_at"`
}

func NewAccessKeyResource() resource.Resource { return &AccessKeyResource{} }

func (r *AccessKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_access_key"
}

func (r *AccessKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Credencial (access key + secret) de um kubmix_iam_user. E o que o SigV4 do " +
			"s3.cloud.kubmix.com exige para enviar objeto. Sem recurso de Update: todo campo forca " +
			"recriacao, porque a API nao altera chave — ela cria outra. Limite de 2 chaves por usuario.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"iam_user_id": schema.StringAttribute{
				Description: "Id do kubmix_iam_user dono da chave.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"access_key_id": schema.StringAttribute{
				Description: "Identificador publico (AKIA...). Pode aparecer em log e em output.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"secret_access_key": schema.StringAttribute{
				Description: "O segredo. A API devolve ESTE valor UMA unica vez, na criacao: o GET nunca " +
					"ecoa o segredo. Por isso ele so existe no state do Terraform — se o state for " +
					"perdido, a chave nao e recuperavel, apenas substituivel. Proteja o backend do state.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status":     schema.StringAttribute{Computed: true},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *AccessKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AccessKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AccessKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Post(
		fmt.Sprintf("/iam/users/%s/access-keys", plan.IamUserID.ValueString()),
		map[string]interface{}{},
	)
	if err != nil {
		resp.Diagnostics.AddError("Error creating access key", err.Error())
		return
	}
	if code == 409 {
		resp.Diagnostics.AddError("Limite de chaves atingido",
			"O usuario IAM ja tem 2 access keys. Apague uma antes de criar outra (a API recusa a terceira).")
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating access key", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	d := unwrapData(result)
	segredo := getString(d, "secretAccessKey")
	if segredo == "" {
		// Sem isto o apply "passaria" com secret vazio e a credencial seria
		// inutilizavel — e irrecuperavel, porque o GET nao ecoa o segredo.
		resp.Diagnostics.AddError("Resposta sem o segredo",
			"A criacao respondeu sem secretAccessKey. A chave pode ter sido criada no servidor; "+
				"confira no painel e apague a orfa antes de repetir o apply.")
		return
	}
	plan.ID = types.StringValue(getString(d, "id"))
	plan.AccessKeyID = types.StringValue(getString(d, "accessKeyId"))
	plan.SecretAccessKey = types.StringValue(segredo)
	plan.Status = types.StringValue(getString(d, "status"))
	plan.CreatedAt = types.StringValue(getString(d, "createdAt"))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read confere se a chave ainda existe na listagem do usuario. NAO toca em
// secret_access_key: a API nunca devolve o segredo de novo, e sobrescrever
// apagaria do state o unico lugar onde ele existe.
func (r *AccessKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AccessKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/iam/users/%s/access-keys", state.IamUserID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading access keys", err.Error())
		return
	}
	if code == 404 {
		// Usuario sumiu: a chave foi junto.
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading access keys", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var env map[string]interface{}
	if err := json.Unmarshal(respBody, &env); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	lista, _ := env["data"].([]interface{})
	for _, it := range lista {
		k, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		if getString(k, "id") == state.ID.ValueString() || getString(k, "accessKeyId") == state.AccessKeyID.ValueString() {
			state.AccessKeyID = types.StringValue(getString(k, "accessKeyId"))
			if v := getString(k, "status"); v != "" {
				state.Status = types.StringValue(v)
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	// Apagada por fora (painel/API): sai do state para o apply seguinte criar outra.
	resp.State.RemoveResource(ctx)
}

// Update nao existe de fato: todo atributo e RequiresReplace ou Computed.
// O framework exige o metodo, entao ele so reescreve o estado.
func (r *AccessKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan AccessKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *AccessKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AccessKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(
		fmt.Sprintf("/iam/users/%s/access-keys/%s", state.IamUserID.ValueString(), state.ID.ValueString()),
	)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting access key", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting access key", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}
