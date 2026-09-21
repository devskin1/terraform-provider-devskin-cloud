package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SSLCertificateResource{}
	_ resource.ResourceWithConfigure   = &SSLCertificateResource{}
	_ resource.ResourceWithImportState = &SSLCertificateResource{}
)

type SSLCertificateResource struct{ client *ApiClient }

type SSLCertificateResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Domain            types.String `tfsdk:"domain"`
	ValidationMethod  types.String `tfsdk:"validation_method"`
	Status            types.String `tfsdk:"status"`
	ValidationRecords types.List   `tfsdk:"validation_records"`
}

var sslCertificateValidationRecordAttrTypes = map[string]attr.Type{
	"name":  types.StringType,
	"value": types.StringType,
}

func NewSSLCertificateResource() resource.Resource { return &SSLCertificateResource{} }

func (r *SSLCertificateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssl_certificate"
}

func (r *SSLCertificateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Certificado TLS solicitado via Let's Encrypt.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain": schema.StringAttribute{
				Description: "Dominio ou wildcard que recebera o certificado.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"validation_method": schema.StringAttribute{
				Description: "Metodo de validacao ACME. A API informa DNS como metodo suportado.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("DNS"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{Computed: true},
			"validation_records": schema.ListNestedAttribute{
				Description: "Registros DNS exigidos para concluir a validacao do certificado.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":  schema.StringAttribute{Computed: true},
						"value": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (r *SSLCertificateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SSLCertificateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SSLCertificateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, code, err := r.client.Post("/security/certificates", map[string]interface{}{
		"domainName":       plan.Domain.ValueString(),
		"validationMethod": plan.ValidationMethod.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating SSL certificate", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating SSL certificate", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing SSL certificate response", err.Error())
		return
	}
	resp.Diagnostics.Append(applySSLCertificate(&plan, unwrapData(result))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SSLCertificateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SSLCertificateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, code, err := r.client.Get(fmt.Sprintf("/security/certificates/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading SSL certificate", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading SSL certificate", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing SSL certificate response", err.Error())
		return
	}
	resp.Diagnostics.Append(applySSLCertificate(&state, unwrapData(result))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *SSLCertificateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SSLCertificateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	plan.Status = state.Status
	plan.ValidationRecords = state.ValidationRecords
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SSLCertificateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SSLCertificateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, code, err := r.client.Delete(fmt.Sprintf("/security/certificates/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting SSL certificate", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting SSL certificate", fmt.Sprintf("Status %d: %s", code, string(respBody)))
	}
}

func (r *SSLCertificateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func applySSLCertificate(m *SSLCertificateResourceModel, data map[string]interface{}) (diags diag.Diagnostics) {
	m.ID = types.StringValue(getString(data, "id"))
	domain := getString(data, "domainName")
	if domain == "" {
		domain = getString(data, "domain")
	}
	m.Domain = types.StringValue(domain)
	m.Status = types.StringValue(getString(data, "status"))
	if method := getString(data, "validationMethod"); method != "" {
		m.ValidationMethod = types.StringValue(method)
	}

	records, ok := data["validationRecords"].([]interface{})
	if !ok {
		list, listDiags := types.ListValue(types.ObjectType{AttrTypes: sslCertificateValidationRecordAttrTypes}, []attr.Value{})
		m.ValidationRecords = list
		diags.Append(listDiags...)
		return diags
	}
	values := make([]attr.Value, 0, len(records))
	for _, raw := range records {
		record, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		value, recordDiags := types.ObjectValue(sslCertificateValidationRecordAttrTypes, map[string]attr.Value{
			"name":  types.StringValue(getString(record, "name")),
			"value": types.StringValue(getString(record, "value")),
		})
		diags.Append(recordDiags...)
		values = append(values, value)
	}
	list, listDiags := types.ListValue(types.ObjectType{AttrTypes: sslCertificateValidationRecordAttrTypes}, values)
	diags.Append(listDiags...)
	m.ValidationRecords = list
	return diags
}
