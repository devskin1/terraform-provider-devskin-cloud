package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &VPCResource{}
	_ resource.ResourceWithConfigure = &VPCResource{}
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
	Name      types.String `tfsdk:"name"`
	CIDRBlock types.String `tfsdk:"cidr_block"`
	Zone      types.String `tfsdk:"zone"`
}

var subnetAttrTypes = map[string]attr.Type{
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
			},
			"cidr_block": schema.StringAttribute{
				Description: "The CIDR block for the VPC (e.g. 10.0.0.0/16).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
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
			},
			"enable_ipv6": schema.BoolAttribute{
				Description: "Whether to enable IPv6 in the VPC.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"subnets": schema.ListNestedAttribute{
				Description: "Subnets to create within the VPC.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "The name of the subnet.",
							Required:    true,
						},
						"cidr_block": schema.StringAttribute{
							Description: "The CIDR block for the subnet.",
							Required:    true,
						},
						"zone": schema.StringAttribute{
							Description: "The availability zone for the subnet.",
							Optional:    true,
						},
					},
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

	body := map[string]interface{}{
		"name":        plan.Name.ValueString(),
		"cidr_block":  plan.CIDRBlock.ValueString(),
		"region":      plan.Region.ValueString(),
		"enable_dns":  plan.EnableDNS.ValueBool(),
		"enable_ipv6": plan.EnableIPv6.ValueBool(),
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

	plan.ID = types.StringValue(getString(result, "id"))
	plan.Status = types.StringValue(getString(result, "status"))
	plan.DefaultSubnetID = types.StringValue(getString(result, "default_subnet_id"))

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

	state.Name = types.StringValue(getString(result, "name"))
	state.CIDRBlock = types.StringValue(getString(result, "cidr_block"))
	state.Region = types.StringValue(getString(result, "region"))
	state.EnableDNS = types.BoolValue(getBool(result, "enable_dns"))
	state.EnableIPv6 = types.BoolValue(getBool(result, "enable_ipv6"))
	state.Status = types.StringValue(getString(result, "status"))
	state.DefaultSubnetID = types.StringValue(getString(result, "default_subnet_id"))

	// Parse subnets from response
	if rawSubnets, ok := result["subnets"].([]interface{}); ok {
		subnetValues := make([]attr.Value, len(rawSubnets))
		for i, rawSubnet := range rawSubnets {
			if s, ok := rawSubnet.(map[string]interface{}); ok {
				zoneVal := types.StringValue(getString(s, "zone"))
				if getString(s, "zone") == "" {
					zoneVal = types.StringNull()
				}
				subnetValues[i], _ = types.ObjectValue(subnetAttrTypes, map[string]attr.Value{
					"name":       types.StringValue(getString(s, "name")),
					"cidr_block": types.StringValue(getString(s, "cidr_block")),
					"zone":       zoneVal,
				})
			}
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
		"name":        plan.Name.ValueString(),
		"enable_dns":  plan.EnableDNS.ValueBool(),
		"enable_ipv6": plan.EnableIPv6.ValueBool(),
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
