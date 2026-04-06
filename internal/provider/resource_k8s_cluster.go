package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &K8sClusterResource{}
	_ resource.ResourceWithConfigure = &K8sClusterResource{}
)

type K8sClusterResource struct {
	client *ApiClient
}

type NodeGroupModel struct {
	Name         types.String `tfsdk:"name"`
	InstanceType types.String `tfsdk:"instance_type"`
	DesiredSize  types.Int64  `tfsdk:"desired_size"`
}

type K8sClusterResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Version    types.String `tfsdk:"version"`
	Region     types.String `tfsdk:"region"`
	NodeGroups types.List   `tfsdk:"node_groups"`
	Status     types.String `tfsdk:"status"`
	Endpoint   types.String `tfsdk:"endpoint"`
	CACert     types.String `tfsdk:"ca_cert"`
}

var nodeGroupAttrTypes = map[string]attr.Type{
	"name":          types.StringType,
	"instance_type": types.StringType,
	"desired_size":  types.Int64Type,
}

func NewK8sClusterResource() resource.Resource {
	return &K8sClusterResource{}
}

func (r *K8sClusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_k8s_cluster"
}

func (r *K8sClusterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud Kubernetes cluster.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the cluster.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the Kubernetes cluster.",
				Required:    true,
			},
			"version": schema.StringAttribute{
				Description: "The Kubernetes version (e.g. 1.28, 1.29).",
				Required:    true,
			},
			"region": schema.StringAttribute{
				Description: "The region where the cluster will be created.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"node_groups": schema.ListNestedAttribute{
				Description: "The node groups for the cluster.",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "The name of the node group.",
							Required:    true,
						},
						"instance_type": schema.StringAttribute{
							Description: "The instance type for nodes in this group.",
							Required:    true,
						},
						"desired_size": schema.Int64Attribute{
							Description: "The desired number of nodes in this group.",
							Required:    true,
						},
					},
				},
			},
			"status": schema.StringAttribute{
				Description: "The current status of the cluster.",
				Computed:    true,
			},
			"endpoint": schema.StringAttribute{
				Description: "The API server endpoint of the cluster.",
				Computed:    true,
			},
			"ca_cert": schema.StringAttribute{
				Description: "The base64-encoded CA certificate for the cluster.",
				Computed:    true,
				Sensitive:   true,
			},
		},
	}
}

func (r *K8sClusterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *K8sClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan K8sClusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var nodeGroups []NodeGroupModel
	resp.Diagnostics.Append(plan.NodeGroups.ElementsAs(ctx, &nodeGroups, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ngPayload := make([]map[string]interface{}, len(nodeGroups))
	for i, ng := range nodeGroups {
		ngPayload[i] = map[string]interface{}{
			"name":          ng.Name.ValueString(),
			"instance_type": ng.InstanceType.ValueString(),
			"desired_size":  ng.DesiredSize.ValueInt64(),
		}
	}

	body := map[string]interface{}{
		"name":        plan.Name.ValueString(),
		"version":     plan.Version.ValueString(),
		"region":      plan.Region.ValueString(),
		"node_groups": ngPayload,
	}

	respBody, statusCode, err := r.client.Post("/k8s/clusters", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating K8s cluster", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating K8s cluster",
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
	plan.Endpoint = types.StringValue(getString(result, "endpoint"))
	plan.CACert = types.StringValue(getString(result, "ca_cert"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *K8sClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state K8sClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/k8s/clusters/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading K8s cluster", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading K8s cluster",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	state.Name = types.StringValue(getString(result, "name"))
	state.Version = types.StringValue(getString(result, "version"))
	state.Region = types.StringValue(getString(result, "region"))
	state.Status = types.StringValue(getString(result, "status"))
	state.Endpoint = types.StringValue(getString(result, "endpoint"))
	state.CACert = types.StringValue(getString(result, "ca_cert"))

	// Parse node_groups from API response
	if rawNGs, ok := result["node_groups"].([]interface{}); ok {
		ngValues := make([]attr.Value, len(rawNGs))
		for i, rawNG := range rawNGs {
			if ng, ok := rawNG.(map[string]interface{}); ok {
				ngValues[i], _ = types.ObjectValue(nodeGroupAttrTypes, map[string]attr.Value{
					"name":          types.StringValue(getString(ng, "name")),
					"instance_type": types.StringValue(getString(ng, "instance_type")),
					"desired_size":  types.Int64Value(getInt64(ng, "desired_size")),
				})
			}
		}
		ngList, diags := types.ListValue(types.ObjectType{AttrTypes: nodeGroupAttrTypes}, ngValues)
		resp.Diagnostics.Append(diags...)
		state.NodeGroups = ngList
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *K8sClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan K8sClusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state K8sClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var nodeGroups []NodeGroupModel
	resp.Diagnostics.Append(plan.NodeGroups.ElementsAs(ctx, &nodeGroups, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ngPayload := make([]map[string]interface{}, len(nodeGroups))
	for i, ng := range nodeGroups {
		ngPayload[i] = map[string]interface{}{
			"name":          ng.Name.ValueString(),
			"instance_type": ng.InstanceType.ValueString(),
			"desired_size":  ng.DesiredSize.ValueInt64(),
		}
	}

	body := map[string]interface{}{
		"name":        plan.Name.ValueString(),
		"version":     plan.Version.ValueString(),
		"node_groups": ngPayload,
	}

	respBody, statusCode, err := r.client.Put(fmt.Sprintf("/k8s/clusters/%s", state.ID.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating K8s cluster", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error updating K8s cluster",
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
	plan.Endpoint = types.StringValue(getString(result, "endpoint"))
	plan.CACert = types.StringValue(getString(result, "ca_cert"))

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *K8sClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state K8sClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/k8s/clusters/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting K8s cluster", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error deleting K8s cluster",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
}
