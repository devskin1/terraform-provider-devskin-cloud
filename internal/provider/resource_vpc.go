package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &VPCResource{}
	_ resource.ResourceWithConfigure   = &VPCResource{}
	_ resource.ResourceWithImportState = &VPCResource{}
)

type VPCResource struct {
	client *ApiClient
}

type VPCResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	CIDRBlock       types.String `tfsdk:"cidr_block"`
	Region          types.String `tfsdk:"region"`
	EnableDNS       types.Bool   `tfsdk:"enable_dns"`
	EnableIPv6      types.Bool   `tfsdk:"enable_ipv6"`
	Status          types.String `tfsdk:"status"`
	DefaultSubnetID types.String `tfsdk:"default_subnet_id"`
	Subnets         types.List   `tfsdk:"subnets"`
}

type SubnetModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	CIDRBlock types.String `tfsdk:"cidr_block"`
	Zone      types.String `tfsdk:"zone"`
}

var subnetAttrTypes = map[string]attr.Type{
	"id":         types.StringType,
	"name":       types.StringType,
	"cidr_block": types.StringType,
	"zone":       types.StringType,
}

func NewVPCResource() resource.Resource {
	return &VPCResource{}
}

func (r *VPCResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

func (r *VPCResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud Virtual Private Cloud (VPC).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the VPC.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the VPC.",
				Required:    true,
				// The API has no PUT/PATCH for VPCs (only GET/POST/DELETE), so
				// any change has to go through a replace instead of an update
				// that would 404 with "Route not found".
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cidr_block": schema.StringAttribute{
				Description: "CIDR da VPC (ex: 10.0.190.0/24). Omitido, o backend aloca o proximo /24 livre — recomendado, evita colidir com VPC de outro tenant.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"region": schema.StringAttribute{
				Description: "The region for the VPC.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"enable_dns": schema.BoolAttribute{
				Description: "Whether to enable DNS support in the VPC.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"enable_ipv6": schema.BoolAttribute{
				Description: "Whether to enable IPv6 in the VPC.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Description: "The current status of the VPC.",
				Computed:    true,
			},
			"default_subnet_id": schema.StringAttribute{
				Description: "The ID of the default subnet in the VPC.",
				Computed:    true,
			},
		},
		// Repeated `subnets { ... }` blocks — the form the docs and examples
		// always used. As a ListNestedAttribute it required `subnets = [{...}]`,
		// so every example in the repo failed with "Blocks of type subnets are
		// not expected here".
		Blocks: map[string]schema.Block{
			"subnets": schema.ListNestedBlock{
				Description: "Extra subnets to create within the VPC. One default subnet is always created.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The ID of the created subnet — use it as subnet_id on an instance.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of the subnet.",
							Required:    true,
						},
						"cidr_block": schema.StringAttribute{
							Description: "The CIDR block for the subnet, inside the VPC CIDR.",
							Required:    true,
						},
						"zone": schema.StringAttribute{
							Description: "The availability zone for the subnet.",
							Optional:    true,
						},
					},
				},
			},
		},
	}
}

func (r *VPCResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *VPCResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VPCResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// createVpcSchema uses camelCase and splits DNS into two flags.
	body := map[string]interface{}{
		"name":               plan.Name.ValueString(),
		"region":             plan.Region.ValueString(),
		"enableIpv6":         plan.EnableIPv6.ValueBool(),
		"enableDnsSupport":   plan.EnableDNS.ValueBool(),
		"enableDnsHostnames": plan.EnableDNS.ValueBool(),
	}
	// cidrBlock is optional upstream: omitted, the backend allocates the next
	// free /24. Sending an empty string would fail the regex.
	if !plan.CIDRBlock.IsNull() && !plan.CIDRBlock.IsUnknown() && plan.CIDRBlock.ValueString() != "" {
		body["cidrBlock"] = plan.CIDRBlock.ValueString()
	}

	respBody, statusCode, err := r.client.Post("/networking/vpcs", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating VPC", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating VPC",
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
	if plan.CIDRBlock.IsNull() || plan.CIDRBlock.IsUnknown() {
		plan.CIDRBlock = types.StringValue(getString(data, "cidrBlock"))
	}

	// There is no `defaultSubnetId` field: the response carries a `subnets`
	// array, and at this point it holds exactly the one subnet the backend
	// auto-creates. Reading the non-existent field left subnet_id empty and
	// every instance failed with SUBNET_VPC_MISMATCH.
	plan.DefaultSubnetID = types.StringNull()
	if raw, ok := data["subnets"].([]interface{}); ok && len(raw) > 0 {
		if first, ok := raw[0].(map[string]interface{}); ok {
			plan.DefaultSubnetID = types.StringValue(getString(first, "id"))
		}
	}

	// The VPC create endpoint ignores a subnet list and always provisions one
	// default subnet. Extra subnets have to be POSTed one by one, otherwise the
	// `subnets` block silently does nothing.
	if !plan.Subnets.IsNull() && !plan.Subnets.IsUnknown() {
		var subnets []SubnetModel
		resp.Diagnostics.Append(plan.Subnets.ElementsAs(ctx, &subnets, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		created := make([]string, 0, len(subnets))
		for _, s := range subnets {
			subnetBody := map[string]interface{}{
				"name":      s.Name.ValueString(),
				"vpcId":     plan.ID.ValueString(),
				"cidrBlock": s.CIDRBlock.ValueString(),
				"isPublic":  true,
			}
			if !s.Zone.IsNull() && !s.Zone.IsUnknown() {
				subnetBody["availabilityZone"] = s.Zone.ValueString()
			}
			sBody, sStatus, sErr := r.client.Post("/networking/subnets", subnetBody)
			if sErr != nil {
				resp.Diagnostics.AddError("Error creating subnet", sErr.Error())
				return
			}
			if sStatus < 200 || sStatus >= 300 {
				resp.Diagnostics.AddError("API error creating subnet",
					fmt.Sprintf("subnet %q: status %d: %s", s.Name.ValueString(), sStatus, string(sBody)))
				return
			}
			// Carry the new subnet id back into state so the config can send an
			// instance to a specific subnet (vpc.subnets[N].id).
			var sResult map[string]interface{}
			if err := json.Unmarshal(sBody, &sResult); err == nil {
				created = append(created, getString(unwrapData(sResult), "id"))
			} else {
				created = append(created, "")
			}
		}

		subnetValues := make([]attr.Value, len(subnets))
		for i, s := range subnets {
			zone := s.Zone
			if zone.IsUnknown() {
				zone = types.StringNull()
			}
			subnetValues[i], _ = types.ObjectValue(subnetAttrTypes, map[string]attr.Value{
				"id":         types.StringValue(created[i]),
				"name":       s.Name,
				"cidr_block": s.CIDRBlock,
				"zone":       zone,
			})
		}
		subnetList, diags := types.ListValue(types.ObjectType{AttrTypes: subnetAttrTypes}, subnetValues)
		resp.Diagnostics.Append(diags...)
		plan.Subnets = subnetList
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *VPCResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VPCResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/networking/vpcs/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading VPC", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading VPC",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	result = unwrapData(result)

	// Backend keys are camelCase; reading snake_case here always yielded empty
	// values and permanent drift.
	state.Name = types.StringValue(getString(result, "name"))
	state.CIDRBlock = types.StringValue(getString(result, "cidrBlock"))
	state.Region = types.StringValue(getString(result, "region"))
	state.EnableDNS = types.BoolValue(getBool(result, "enableDnsSupport"))
	state.EnableIPv6 = types.BoolValue(getBool(result, "enableIpv6"))
	state.Status = types.StringValue(getString(result, "status"))
	// Same as in Create: derive the default subnet from the subnets array.
	// The backend names it "<vpc name>-subnet-1".
	state.DefaultSubnetID = types.StringNull()
	if raw, ok := result["subnets"].([]interface{}); ok {
		for _, item := range raw {
			sub, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if getString(sub, "name") == getString(result, "name")+"-subnet-1" {
				state.DefaultSubnetID = types.StringValue(getString(sub, "id"))
				break
			}
		}
		if state.DefaultSubnetID.IsNull() && len(raw) > 0 {
			if first, ok := raw[0].(map[string]interface{}); ok {
				state.DefaultSubnetID = types.StringValue(getString(first, "id"))
			}
		}
	}

	// Reconcile subnets against what the config declared. The API returns the
	// backend's auto-created default subnet too; copying the raw list in made
	// Terraform see a permanent diff (an extra block to destroy and one to
	// rename) on every plan. Match by name and keep the declared order.
	if raw, ok := result["subnets"].([]interface{}); ok && !state.Subnets.IsNull() {
		remoto := map[string]map[string]interface{}{}
		for _, item := range raw {
			if sub, ok := item.(map[string]interface{}); ok {
				remoto[getString(sub, "name")] = sub
			}
		}
		var declaradas []SubnetModel
		state.Subnets.ElementsAs(ctx, &declaradas, false)
		subnetValues := make([]attr.Value, 0, len(declaradas))
		for _, d := range declaradas {
			sub, existe := remoto[d.Name.ValueString()]
			if !existe {
				// sumiu no lado do servidor: mantem o que estava no state para
				// o plano acusar a recriacao
				subnetValues = append(subnetValues, mustSubnetObject(d.ID, d.Name, d.CIDRBlock, d.Zone))
				continue
			}
			zona := types.StringValue(getString(sub, "availabilityZone"))
			if getString(sub, "availabilityZone") == "" {
				zona = d.Zone
			}
			subnetValues = append(subnetValues, mustSubnetObject(
				types.StringValue(getString(sub, "id")),
				types.StringValue(getString(sub, "name")),
				types.StringValue(getString(sub, "cidrBlock")),
				zona,
			))
		}
		subnetList, diags := types.ListValue(types.ObjectType{AttrTypes: subnetAttrTypes}, subnetValues)
		resp.Diagnostics.Append(diags...)
		state.Subnets = subnetList
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *VPCResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan VPCResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state VPCResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name":               plan.Name.ValueString(),
		"enableDnsSupport":   plan.EnableDNS.ValueBool(),
		"enableDnsHostnames": plan.EnableDNS.ValueBool(),
		"enableIpv6":         plan.EnableIPv6.ValueBool(),
	}

	if !plan.Subnets.IsNull() && !plan.Subnets.IsUnknown() {
		var subnets []SubnetModel
		resp.Diagnostics.Append(plan.Subnets.ElementsAs(ctx, &subnets, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		subnetPayload := make([]map[string]interface{}, len(subnets))
		for i, s := range subnets {
			subnetPayload[i] = map[string]interface{}{
				"name":       s.Name.ValueString(),
				"cidr_block": s.CIDRBlock.ValueString(),
			}
			if !s.Zone.IsNull() && !s.Zone.IsUnknown() {
				subnetPayload[i]["zone"] = s.Zone.ValueString()
			}
		}
		body["subnets"] = subnetPayload
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/networking/vpcs/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating VPC", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating VPC",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	plan.ID = state.ID
	plan.Status = types.StringValue(getString(result, "status"))
	plan.DefaultSubnetID = types.StringValue(getString(result, "default_subnet_id"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *VPCResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VPCResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/networking/vpcs/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting VPC", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting VPC",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}

// mustSubnetObject builds the object value for one subnets block.
func mustSubnetObject(id, name, cidr, zone types.String) attr.Value {
	v, _ := types.ObjectValue(subnetAttrTypes, map[string]attr.Value{
		"id":         id,
		"name":       name,
		"cidr_block": cidr,
		"zone":       zone,
	})
	return v
}

// ImportState permite `terraform import` pelo ID do recurso — essencial para
// readotar infra existente ou recuperar um state perdido, que antes exigia
// destruir tudo na mao e recriar.
func (r *VPCResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
