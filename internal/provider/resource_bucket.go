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
	_ resource.Resource                = &BucketResource{}
	_ resource.ResourceWithConfigure   = &BucketResource{}
	_ resource.ResourceWithImportState = &BucketResource{}
)

type BucketResource struct {
	client *ApiClient
}

// Espelha createBucketSchema/updateBucket em storage.controller.ts.
type BucketResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Region       types.String `tfsdk:"region"`
	Versioning   types.Bool   `tfsdk:"versioning"`
	Encryption   types.String `tfsdk:"encryption"`
	PublicAccess types.Bool   `tfsdk:"public_access"`
	Status       types.String `tfsdk:"status"`
}

func NewBucketResource() resource.Resource {
	return &BucketResource{}
}

func (r *BucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bucket"
}

func (r *BucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Bucket de object storage. O nome e unico na PLATAFORMA inteira (nao por organizacao), " +
			"porque o conteudo e servido por nome em /storage/public/:bucket — mesma regra do S3.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Identificador do bucket.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Nome do bucket (3-63 chars, minusculas, digitos, ponto e hifen). " +
					"Unico em toda a plataforma. Mudar recria o bucket.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Description: "Regiao do bucket. Mudar recria o bucket.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("sa-east-1"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"versioning": schema.BoolAttribute{
				Description: "Mantem versoes anteriores de objeto sobrescrito ou apagado. " +
					"Nao e so uma marca no banco: e propagado para o VersioningConfiguration do storage.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"encryption": schema.StringAttribute{
				Description: "NONE, AES256 ou KMS.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("NONE"),
			},
			"public_access": schema.BoolAttribute{
				Description: "Deixa o conteudo acessivel sem autenticacao. Padrao false — " +
					"mantenha false para bucket com dado de cliente.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"status": schema.StringAttribute{
				Description: "Estado reportado pela plataforma.",
				Computed:    true,
			},
		},
	}
}

func (r *BucketResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*ApiClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *ApiClient, got something else.")
		return
	}
	r.client = client
}

// applyBucket copia a resposta da API para o modelo. Os campos opcionais sao
// Computed, entao TODOS precisam sair preenchidos daqui — deixar um como null
// faz o Terraform acusar "provider produced inconsistent result after apply".
func applyBucket(m *BucketResourceModel, data map[string]interface{}) {
	m.ID = types.StringValue(getString(data, "id"))
	m.Name = types.StringValue(getString(data, "name"))
	m.Status = types.StringValue(getString(data, "status"))
	if v := getString(data, "region"); v != "" {
		m.Region = types.StringValue(v)
	}
	if v := getString(data, "encryption"); v != "" {
		m.Encryption = types.StringValue(v)
	}
	m.Versioning = types.BoolValue(getBool(data, "versioning"))
	m.PublicAccess = types.BoolValue(getBool(data, "publicAccess"))
}

func (r *BucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan BucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":         plan.Name.ValueString(),
		"region":       plan.Region.ValueString(),
		"versioning":   plan.Versioning.ValueBool(),
		"encryption":   plan.Encryption.ValueString(),
		"publicAccess": plan.PublicAccess.ValueBool(),
	}

	respBody, statusCode, err := r.client.Post("/storage/buckets", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating bucket", err.Error())
		return
	}
	// 409 tem mensagem propria e acionavel: o nome e global, entao colisao com
	// outro tenant e o erro mais comum aqui.
	if statusCode == 409 {
		resp.Diagnostics.AddError("Nome de bucket ja em uso",
			fmt.Sprintf("O nome %q ja existe na plataforma (o nome e global, nao por organizacao). Escolha outro. Resposta: %s",
				plan.Name.ValueString(), string(respBody)))
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating bucket",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyBucket(&plan, unwrapData(result))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *BucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/storage/buckets/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading bucket", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading bucket",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyBucket(&state, unwrapData(result))

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *BucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan BucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state BucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID

	// name e region tem RequiresReplace, entao aqui so chegam os mutaveis.
	body := map[string]interface{}{
		"versioning":   plan.Versioning.ValueBool(),
		"encryption":   plan.Encryption.ValueString(),
		"publicAccess": plan.PublicAccess.ValueBool(),
	}

	respBody, statusCode, err := r.client.Patch(fmt.Sprintf("/storage/buckets/%s", plan.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating bucket", err.Error())
		return
	}
	// O backend recusa com 502 quando o storage nao aceita o versionamento —
	// e correto propagar, senao gravariamos no state uma marca que nao vale.
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating bucket",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		applyBucket(&plan, unwrapData(result))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *BucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state BucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/storage/buckets/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting bucket", err.Error())
		return
	}
	if statusCode == 404 {
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting bucket",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}

func (r *BucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
