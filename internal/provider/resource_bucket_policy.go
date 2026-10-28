package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &BucketPolicyResource{}
	_ resource.ResourceWithConfigure = &BucketPolicyResource{}
)

type BucketPolicyResource struct{ client *ApiClient }

type BucketPolicyResourceModel struct {
	ID       types.String `tfsdk:"id"`
	BucketID types.String `tfsdk:"bucket_id"`
	Policy   types.String `tfsdk:"policy"`
}

func NewBucketPolicyResource() resource.Resource { return &BucketPolicyResource{} }

func (r *BucketPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bucket_policy"
}

func (r *BucketPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Policy de um kubmix_bucket, no formato de policy da AWS. " +
			"ATENCAO: a API grava a policy no armazenamento mas NAO a devolve em nenhuma leitura, " +
			"entao o Terraform nao detecta mudanca feita por fora — o state e a unica fonte. " +
			"Quem editar a policy pelo painel nao vai aparecer como drift aqui.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"bucket_id": schema.StringAttribute{
				Description: "Id do kubmix_bucket.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"policy": schema.StringAttribute{
				Description: "Documento JSON da policy. Use jsonencode() para nao brigar com aspas.",
				Required:    true,
			},
		},
	}
}

func (r *BucketPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// aplica manda a policy. Valida o JSON antes: o backend aceita `z.any()`, entao
// uma string quebrada passaria batido e so falharia la no armazenamento, onde
// ninguem ve.
func (r *BucketPolicyResource) aplica(m BucketPolicyResourceModel, diags *diag.Diagnostics) bool {
	var doc interface{}
	if err := json.Unmarshal([]byte(m.Policy.ValueString()), &doc); err != nil {
		diags.AddError("Policy nao e JSON valido", err.Error())
		return false
	}
	respBody, code, err := r.client.Put(
		fmt.Sprintf("/storage/buckets/%s/policy", m.BucketID.ValueString()),
		map[string]interface{}{"policy": doc},
	)
	if err != nil {
		diags.AddError("Error applying bucket policy", err.Error())
		return false
	}
	if code < 200 || code >= 300 {
		diags.AddError("API error applying bucket policy", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return false
	}
	return true
}

func (r *BucketPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan BucketPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !r.aplica(plan, &resp.Diagnostics) {
		return
	}
	plan.ID = types.StringValue(plan.BucketID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read so confirma que o bucket ainda existe. A policy em si nao e lida porque
// a API nao a devolve (ver a descricao do recurso).
func (r *BucketPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BucketPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/storage/buckets/%s", state.BucketID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading bucket", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading bucket", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		if recursoFoiDestruido(getString(unwrapData(result), "status")) {
			resp.State.RemoveResource(ctx)
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *BucketPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state BucketPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	if !r.aplica(plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete grava uma policy vazia: a API nao tem rota de remocao, e deixar a
// policy antiga valendo depois do destroy manteria permissao que o cliente
// achou que tinha removido.
func (r *BucketPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state BucketPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, code, err := r.client.Put(
		fmt.Sprintf("/storage/buckets/%s/policy", state.BucketID.ValueString()),
		map[string]interface{}{"policy": map[string]interface{}{"Version": "2012-10-17", "Statement": []interface{}{}}},
	)
	if err != nil {
		resp.Diagnostics.AddWarning("Policy pode ter ficado para tras",
			fmt.Sprintf("Nao consegui limpar a policy do bucket %s: %s. Confira no painel.", state.BucketID.ValueString(), err.Error()))
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddWarning("Policy pode ter ficado para tras",
			fmt.Sprintf("Limpeza da policy do bucket %s respondeu %d. Confira no painel.", state.BucketID.ValueString(), code))
	}
}
