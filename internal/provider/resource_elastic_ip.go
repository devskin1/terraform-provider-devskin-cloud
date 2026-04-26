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
	_ resource.Resource              = &ElasticIPResource{}
	_ resource.ResourceWithConfigure = &ElasticIPResource{}
)

type ElasticIPResource struct {
	client *ApiClient
}

type ElasticIPResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Region              types.String `tfsdk:"region"`
	Description         types.String `tfsdk:"description"`
	IPVersion           types.String `tfsdk:"ip_version"`
	IPAddress           types.String `tfsdk:"ip_address"`
	InstanceID          types.String `tfsdk:"instance_id"`
	KubernetesClusterID types.String `tfsdk:"kubernetes_cluster_id"`
	NodeName            types.String `tfsdk:"node_name"`
	Status              types.String `tfsdk:"status"`
}

func NewElasticIPResource() resource.Resource {
	return &ElasticIPResource{}
}

func (r *ElasticIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_elastic_ip"
}

func (r *ElasticIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud Elastic IP. Optionally associates it with either a regular VM Instance OR a Kubernetes node (master/worker). When associated with a K8s node, ports 80/443/30080/30443 are auto-opened on pfSense.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"region":      schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"description": schema.StringAttribute{Optional: true},
			"ip_version":  schema.StringAttribute{Optional: true, Computed: true, Description: "\"IPv4\" (default) or \"IPv6\""},
			"ip_address":  schema.StringAttribute{Computed: true, Description: "The allocated public IP."},
			"instance_id": schema.StringAttribute{Optional: true, Description: "Target VM instance id (mutually exclusive with kubernetes_cluster_id)"},
			"kubernetes_cluster_id": schema.StringAttribute{Optional: true, Description: "Target K8s cluster (use with node_name)"},
			"node_name":             schema.StringAttribute{Optional: true, Description: "K8s node name from cluster.tags.vmIps (e.g. master-100, worker-101)"},
			"status":                schema.StringAttribute{Computed: true},
		},
	}
}

func (r *ElasticIPResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*ApiClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *ApiClient.")
		return
	}
	r.client = client
}

func (r *ElasticIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ElasticIPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	allocBody := map[string]interface{}{
		"region": plan.Region.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		allocBody["description"] = plan.Description.ValueString()
	}
	if !plan.IPVersion.IsNull() && !plan.IPVersion.IsUnknown() {
		allocBody["ipVersion"] = plan.IPVersion.ValueString()
	}

	respBody, statusCode, err := r.client.Post("/networking/elastic-ips", allocBody)
	if err != nil {
		resp.Diagnostics.AddError("Error allocating elastic IP", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error allocating elastic IP", fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	plan.ID = types.StringValue(getString(result, "id"))
	plan.IPAddress = types.StringValue(getString(result, "ipAddress"))
	plan.IPVersion = types.StringValue(getString(result, "ipVersion"))
	plan.Status = types.StringValue(getString(result, "status"))

	// Optional associate
	if (!plan.InstanceID.IsNull() && !plan.InstanceID.IsUnknown()) ||
		(!plan.KubernetesClusterID.IsNull() && !plan.KubernetesClusterID.IsUnknown()) {
		assoc := map[string]interface{}{}
		if !plan.InstanceID.IsNull() && plan.InstanceID.ValueString() != "" {
			assoc["instanceId"] = plan.InstanceID.ValueString()
		}
		if !plan.KubernetesClusterID.IsNull() && plan.KubernetesClusterID.ValueString() != "" {
			assoc["kubernetesClusterId"] = plan.KubernetesClusterID.ValueString()
			if !plan.NodeName.IsNull() {
				assoc["nodeName"] = plan.NodeName.ValueString()
			}
		}
		_, sc, aerr := r.client.Post(fmt.Sprintf("/networking/elastic-ips/%s/associate", plan.ID.ValueString()), assoc)
		if aerr != nil {
			resp.Diagnostics.AddError("Error associating elastic IP", aerr.Error())
			return
		}
		if sc < 200 || sc >= 300 {
			resp.Diagnostics.AddWarning("Elastic IP associate failed",
				fmt.Sprintf("Allocation succeeded but associate returned %d. Run terraform apply again or associate manually.", sc))
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ElasticIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ElasticIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/networking/elastic-ips/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading elastic IP", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading elastic IP", fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	state.IPAddress = types.StringValue(getString(result, "ipAddress"))
	state.IPVersion = types.StringValue(getString(result, "ipVersion"))
	state.Status = types.StringValue(getString(result, "status"))
	if v := getString(result, "instanceId"); v != "" {
		state.InstanceID = types.StringValue(v)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ElasticIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Re-association (instance switch) handled here in a future patch. For now,
	// updates other than description are no-ops.
	var plan ElasticIPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ElasticIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ElasticIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/networking/elastic-ips/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error releasing elastic IP", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 && statusCode != 404 {
		resp.Diagnostics.AddError("API error releasing elastic IP", fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
	}
}
