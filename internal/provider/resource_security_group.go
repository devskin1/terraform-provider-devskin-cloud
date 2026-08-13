package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SecurityGroupResource{}
	_ resource.ResourceWithConfigure   = &SecurityGroupResource{}
	_ resource.ResourceWithImportState = &SecurityGroupResource{}
)

type SecurityGroupResource struct {
	client *ApiClient
}

type SecurityGroupResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	VPCID        types.String `tfsdk:"vpc_id"`
	InboundRules types.List   `tfsdk:"inbound_rule"`
	OutboundRule types.List   `tfsdk:"outbound_rule"`
	Status       types.String `tfsdk:"status"`
}

// Mirrors ruleSchema in networking.controller.ts:
// { protocol: tcp|udp|icmp|all, port, source, description, iface: wan|lan }
type SGRuleModel struct {
	Protocol    types.String `tfsdk:"protocol"`
	Port        types.String `tfsdk:"port"`
	Source      types.String `tfsdk:"source"`
	Description types.String `tfsdk:"description"`
	Iface       types.String `tfsdk:"iface"`
}

var sgRuleAttrTypes = map[string]attr.Type{
	"protocol":    types.StringType,
	"port":        types.StringType,
	"source":      types.StringType,
	"description": types.StringType,
	"iface":       types.StringType,
}

func NewSecurityGroupResource() resource.Resource {
	return &SecurityGroupResource{}
}

func (r *SecurityGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_group"
}

func sgRuleBlock(desc string) schema.ListNestedBlock {
	return schema.ListNestedBlock{
		Description: desc,
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"protocol": schema.StringAttribute{
					Description: "tcp, udp, icmp or all.",
					Required:    true,
				},
				"port": schema.StringAttribute{
					Description: "Port or range (e.g. \"443\", \"8000-8100\"). Omit for protocol=all.",
					Optional:    true,
				},
				"source": schema.StringAttribute{
					Description: "Source CIDR (e.g. 0.0.0.0/0). Omit to allow any.",
					Optional:    true,
				},
				"description": schema.StringAttribute{
					Description: "Free-text description of the rule.",
					Optional:    true,
				},
				"iface": schema.StringAttribute{
					Description: "wan for external traffic, lan for internal. Defaults to wan.",
					Optional:    true,
				},
			},
		},
	}
}

func (r *SecurityGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud security group. Rules are applied as pfSense firewall rules for the VPC.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the security group.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the security group.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the security group.",
				Optional:    true,
			},
			"vpc_id": schema.StringAttribute{
				Description: "The VPC this security group belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Description: "The current status of the security group.",
				Computed:    true,
			},
		},
		Blocks: map[string]schema.Block{
			"inbound_rule":  sgRuleBlock("Inbound (ingress) rules."),
			"outbound_rule": sgRuleBlock("Outbound (egress) rules."),
		},
	}
}

func (r *SecurityGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func rulesToPayload(ctx context.Context, list types.List) []map[string]interface{} {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var rules []SGRuleModel
	list.ElementsAs(ctx, &rules, false)
	out := make([]map[string]interface{}, 0, len(rules))
	for _, rule := range rules {
		item := map[string]interface{}{"protocol": rule.Protocol.ValueString()}
		if !rule.Port.IsNull() && !rule.Port.IsUnknown() && rule.Port.ValueString() != "" {
			item["port"] = rule.Port.ValueString()
		}
		if !rule.Source.IsNull() && !rule.Source.IsUnknown() && rule.Source.ValueString() != "" {
			item["source"] = rule.Source.ValueString()
		}
		if !rule.Description.IsNull() && !rule.Description.IsUnknown() {
			item["description"] = rule.Description.ValueString()
		}
		if !rule.Iface.IsNull() && !rule.Iface.IsUnknown() && rule.Iface.ValueString() != "" {
			item["iface"] = rule.Iface.ValueString()
		}
		out = append(out, item)
	}
	return out
}

func (r *SecurityGroupResource) buildBody(ctx context.Context, plan SecurityGroupResourceModel) map[string]interface{} {
	body := map[string]interface{}{
		"name":  plan.Name.ValueString(),
		"vpcId": plan.VPCID.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		body["description"] = plan.Description.ValueString()
	}
	if in := rulesToPayload(ctx, plan.InboundRules); in != nil {
		body["inboundRules"] = in
	}
	if out := rulesToPayload(ctx, plan.OutboundRule); out != nil {
		body["outboundRules"] = out
	}
	return body
}

func (r *SecurityGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecurityGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Post("/networking/security-groups", r.buildBody(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating security group", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating security group",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	data := unwrapData(result)

	plan.ID = types.StringValue(getString(data, "id"))
	plan.Status = types.StringValue(getString(data, "status"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SecurityGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecurityGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/networking/security-groups/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading security group", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading security group",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	data := unwrapData(result)

	state.Name = types.StringValue(getString(data, "name"))
	state.Status = types.StringValue(getString(data, "status"))
	if d := getString(data, "description"); d != "" {
		state.Description = types.StringValue(d)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *SecurityGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SecurityGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state SecurityGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The backend exposes PATCH for security groups; PUT is not routed.
	respBody, statusCode, err := r.client.Do("PATCH",
		fmt.Sprintf("/networking/security-groups/%s", state.ID.ValueString()), r.buildBody(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating security group", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating security group",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	plan.ID = state.ID
	if plan.Status.IsNull() || plan.Status.IsUnknown() {
		plan.Status = state.Status
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SecurityGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecurityGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/networking/security-groups/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting security group", err.Error())
		return
	}
	if statusCode == 404 {
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting security group",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}

// ImportState permite `terraform import` pelo ID do recurso — essencial para
// readotar infra existente ou recuperar um state perdido, que antes exigia
// destruir tudo na mao e recriar.
func (r *SecurityGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
