package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &DNSRecordResource{}
	_ resource.ResourceWithConfigure   = &DNSRecordResource{}
	_ resource.ResourceWithImportState = &DNSRecordResource{}
)

type DNSRecordResource struct{ client *ApiClient }

// Espelha createRecordSchema em dns.controller.ts.
type DNSRecordResourceModel struct {
	ID     types.String `tfsdk:"id"`
	ZoneID types.String `tfsdk:"zone_id"`
	Name   types.String `tfsdk:"name"`
	Type   types.String `tfsdk:"type"`
	TTL    types.Int64  `tfsdk:"ttl"`
	Values types.List   `tfsdk:"values"`
}

func NewDNSRecordResource() resource.Resource { return &DNSRecordResource{} }

func (r *DNSRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (r *DNSRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Registro DNS dentro de uma zona hospedada.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"zone_id": schema.StringAttribute{
				Description: "Zona onde o registro vive. Mudar recria.",
				Required:    true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Description: "Nome do registro.", Required: true},
			"type": schema.StringAttribute{
				Description: "A, AAAA, CNAME, MX, TXT, NS, SRV ou CAA. Mudar recria.",
				Required:    true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ttl": schema.Int64Attribute{Description: "Segundos, de 0 a 86400.", Optional: true, Computed: true, Default: int64default.StaticInt64(300)},
			"values": schema.ListAttribute{
				Description: "Valores do registro. Ao menos um.",
				ElementType: types.StringType,
				Required:    true,
			},
		},
	}
}

func (r *DNSRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DNSRecordResource) body(ctx context.Context, m DNSRecordResourceModel) map[string]interface{} {
	vals := []string{}
	if !m.Values.IsNull() && !m.Values.IsUnknown() {
		m.Values.ElementsAs(ctx, &vals, false)
	}
	return map[string]interface{}{
		"name":   m.Name.ValueString(),
		"type":   m.Type.ValueString(),
		"ttl":    m.TTL.ValueInt64(),
		"values": vals,
	}
}

func (r *DNSRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DNSRecordResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Post(fmt.Sprintf("/dns/zones/%s/records", plan.ZoneID.ValueString()), r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating dns record", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating dns record", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	d := unwrapData(result)
	plan.ID = types.StringValue(getString(d, "id"))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read percorre a lista da zona: nao existe rota GET de registro individual.
func (r *DNSRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DNSRecordResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/dns/zones/%s/records", state.ZoneID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading dns records", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading dns records", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	arr, _ := result["data"].([]interface{})
	achou := false
	for _, it := range arr {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		if getString(m, "id") == state.ID.ValueString() {
			achou = true
			state.Name = types.StringValue(getString(m, "name"))
			state.Type = types.StringValue(strings.ToUpper(getString(m, "type")))
			if v := getInt64(m, "ttl"); v > 0 {
				state.TTL = types.Int64Value(v)
			}
			break
		}
	}
	if !achou {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *DNSRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DNSRecordResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	respBody, code, err := r.client.Put(
		fmt.Sprintf("/dns/zones/%s/records/%s", plan.ZoneID.ValueString(), plan.ID.ValueString()), r.body(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating dns record", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error updating dns record", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DNSRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DNSRecordResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(
		fmt.Sprintf("/dns/zones/%s/records/%s", state.ZoneID.ValueString(), state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting dns record", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting dns record", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}

// Import no formato "<zone_id>/<record_id>", porque o registro so pode ser
// encontrado dentro da zona — o id sozinho nao basta.
func (r *DNSRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	partes := strings.SplitN(req.ID, "/", 2)
	if len(partes) != 2 || partes[0] == "" || partes[1] == "" {
		resp.Diagnostics.AddError("Formato de import invalido",
			"Use: terraform import kubmix_dns_record.exemplo <zone_id>/<record_id>")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), partes[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), partes[1])...)
}
