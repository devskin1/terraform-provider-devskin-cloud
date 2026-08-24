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
	_ resource.Resource                = &LoadBalancerResource{}
	_ resource.ResourceWithConfigure   = &LoadBalancerResource{}
	_ resource.ResourceWithImportState = &LoadBalancerResource{}
)

type LoadBalancerResource struct{ client *ApiClient }

// Espelha createLoadBalancer em networking.controller.ts.
type LoadBalancerResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	VPCID     types.String `tfsdk:"vpc_id"`
	SubnetIDs types.List   `tfsdk:"subnet_ids"`
	LBType    types.String `tfsdk:"lb_type"`
	Scheme    types.String `tfsdk:"scheme"`
	Region    types.String `tfsdk:"region"`
	DNSName   types.String `tfsdk:"dns_name"`
	Status    types.String `tfsdk:"status"`
}

func NewLoadBalancerResource() resource.Resource { return &LoadBalancerResource{} }

func (r *LoadBalancerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer"
}

func (r *LoadBalancerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Balanceador de carga. Ele provisiona uma VM de HAProxy por tras — destruir o recurso destroi essa VM e libera o IP.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Description: "Nome, unico na organizacao. Mudar recria.", Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"vpc_id": schema.StringAttribute{Description: "VPC onde o balanceador vive. Mudar recria.", Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"subnet_ids": schema.ListAttribute{
				Description: "Sub-redes atendidas.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"lb_type": schema.StringAttribute{
				Description: "APPLICATION (L7), NETWORK (L4) ou GATEWAY. Mudar recria.",
				Optional:    true, Computed: true,
				Default:       stringdefault.StaticString("APPLICATION"),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"scheme": schema.StringAttribute{
				Description: "internet-facing (exposto) ou internal (so rede privada). Mudar recria.",
				Optional:    true, Computed: true,
				Default:       stringdefault.StaticString("internet-facing"),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"region":   schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("sa-east-1")},
			"dns_name": schema.StringAttribute{Description: "Nome DNS publicado pela plataforma.", Computed: true},
			"status":   schema.StringAttribute{Computed: true},
		},
	}
}

func (r *LoadBalancerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func applyLoadBalancer(m *LoadBalancerResourceModel, d map[string]interface{}) {
	m.ID = types.StringValue(getString(d, "id"))
	m.Name = types.StringValue(getString(d, "name"))
	m.Status = types.StringValue(getString(d, "status"))
	m.DNSName = types.StringValue(getString(d, "dnsName"))
	if v := getString(d, "region"); v != "" {
		m.Region = types.StringValue(v)
	}
}

func (r *LoadBalancerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LoadBalancerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	subnets := []string{}
	if !plan.SubnetIDs.IsNull() && !plan.SubnetIDs.IsUnknown() {
		plan.SubnetIDs.ElementsAs(ctx, &subnets, false)
	}
	body := map[string]interface{}{
		"name":      plan.Name.ValueString(),
		"vpcId":     plan.VPCID.ValueString(),
		"subnetIds": subnets,
		"lbType":    plan.LBType.ValueString(),
		"scheme":    plan.Scheme.ValueString(),
		"region":    plan.Region.ValueString(),
	}
	respBody, code, err := r.client.Post("/networking/load-balancers", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating load balancer", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error creating load balancer", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyLoadBalancer(&plan, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LoadBalancerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LoadBalancerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Get(fmt.Sprintf("/networking/load-balancers/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading load balancer", err.Error())
		return
	}
	if code == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error reading load balancer", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	applyLoadBalancer(&state, unwrapData(result))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LoadBalancerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state LoadBalancerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	plan.DNSName = state.DNSName
	// Quase tudo tem RequiresReplace; sobram os campos que a rota PATCH aceita.
	body := map[string]interface{}{"name": plan.Name.ValueString()}
	respBody, code, err := r.client.Patch(fmt.Sprintf("/networking/load-balancers/%s", plan.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating load balancer", err.Error())
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error updating load balancer", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		applyLoadBalancer(&plan, unwrapData(result))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LoadBalancerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LoadBalancerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, code, err := r.client.Delete(fmt.Sprintf("/networking/load-balancers/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting load balancer", err.Error())
		return
	}
	if code == 404 {
		return
	}
	if code < 200 || code >= 300 {
		resp.Diagnostics.AddError("API error deleting load balancer", fmt.Sprintf("Status %d: %s", code, string(respBody)))
		return
	}
}

func (r *LoadBalancerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
