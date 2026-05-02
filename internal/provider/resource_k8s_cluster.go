package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Version            types.String `tfsdk:"version"`
	Region             types.String `tfsdk:"region"`
	VPCID              types.String `tfsdk:"vpc_id"`
	NodeGroups         types.List   `tfsdk:"node_groups"`
	MaxPods            types.Int64  `tfsdk:"max_pods"`
	CNI                types.String `tfsdk:"cni"`
	Addons             types.Object `tfsdk:"addons"`
	HAControlPlane     types.Bool   `tfsdk:"ha_control_plane"`
	BackupBucketID     types.String `tfsdk:"backup_bucket_id"`
	BackupRetention    types.Int64  `tfsdk:"backup_retention"`
	AllowedSourceCIDRs types.List   `tfsdk:"allowed_source_cidrs"`
	AutoscalerEnabled  types.Bool   `tfsdk:"autoscaler_enabled"`
	AutoscalerMinNodes types.Int64  `tfsdk:"autoscaler_min_nodes"`
	AutoscalerMaxNodes types.Int64  `tfsdk:"autoscaler_max_nodes"`
	AutohealEnabled    types.Bool   `tfsdk:"autoheal_enabled"`
	Status             types.String `tfsdk:"status"`
	Endpoint           types.String `tfsdk:"endpoint"`
	CACert             types.String `tfsdk:"ca_cert"`
	OIDCIssuer         types.String `tfsdk:"oidc_issuer"`
}

var addonsAttrTypes = map[string]attr.Type{
	"metrics_server":      types.BoolType,
	"ingress_nginx":       types.BoolType,
	"cert_manager":        types.BoolType,
	"kyverno":             types.BoolType,
	"cilium":              types.BoolType,
	"local_path_storage":  types.BoolType,
	"velero":              types.BoolType,
	"irsa":                types.BoolType,
	"longhorn":            types.BoolType,
	"default_deny_netpol": types.BoolType,
}

// addonsBackendKey maps the TF snake_case attribute name to the backend
// camelCase JSON field. Single source of truth, used in Create + Update.
var addonsBackendKey = map[string]string{
	"metrics_server":      "metricsServer",
	"ingress_nginx":       "ingressNginx",
	"cert_manager":        "certManager",
	"kyverno":             "kyverno",
	"cilium":              "cilium",
	"local_path_storage":  "localPathStorage",
	"velero":              "velero",
	"irsa":                "irsa",
	"longhorn":            "longhorn",
	"default_deny_netpol": "defaultDenyNetpol",
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
			"vpc_id": schema.StringAttribute{
				Description: "The VPC ID to place the Kubernetes cluster in.",
				Optional:    true,
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
			"max_pods": schema.Int64Attribute{
				Description: "Max pods per kubelet node. Default 110, range 10-1000.",
				Optional:    true,
				Computed:    true,
			},
			"cni": schema.StringAttribute{
				Description: "CNI plugin: \"calico\" (default) or \"flannel\". Cilium addon overrides this.",
				Optional:    true,
				Computed:    true,
			},
			"ha_control_plane": schema.BoolAttribute{
				Description: "Highly-available control plane: 3 masters + kube-vip VIP for the API server. " +
					"Cannot be toggled after creation — changing this forces resource replacement. Default false.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"backup_bucket_id": schema.StringAttribute{
				Description: "ID of the S3 bucket where periodic etcd snapshots are pushed by the platform " +
					"backup cronjob. Set to enable backups, leave unset to disable. Mutable.",
				Optional: true,
			},
			"backup_retention": schema.Int64Attribute{
				Description: "Number of days etcd snapshots are retained (1-90). Default 7. Mutable.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(7),
				Validators: []validator.Int64{
					retentionRangeValidator{},
				},
			},
			"allowed_source_cidrs": schema.ListAttribute{
				Description: "Source IPv4 CIDR ranges allowed to reach the cluster API server / ingress through pfSense. " +
					"Default [\"0.0.0.0/0\"] (open). Must contain at least one entry. Mutable.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					nonEmptyStringListValidator{},
				},
			},
			"autoscaler_enabled": schema.BoolAttribute{
				Description: "Enable the Proxmox-aware Cluster Autoscaler. When on, worker VMs are added/removed " +
					"based on pending pods and idle node pressure. Mutable — toggle on/off at any time.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"autoscaler_min_nodes": schema.Int64Attribute{
				Description: "Minimum worker count when the autoscaler is enabled. Default 1.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
			},
			"autoscaler_max_nodes": schema.Int64Attribute{
				Description: "Maximum worker count cap. Default 10.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(10),
			},
			"autoheal_enabled": schema.BoolAttribute{
				Description: "Enable Node Auto-Heal: when a worker stops reporting (kubelet down/VM crashed) " +
					"for 5 minutes, the platform destroys the dead VM and provisions a fresh replacement, " +
					"then re-joins it to the cluster. Mutable. Default true (opt-out).",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"addons": schema.SingleNestedAttribute{
				Description: "Optional cluster add-ons installed automatically after master init.",
				Optional:    true,
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"metrics_server":      schema.BoolAttribute{Optional: true, Computed: true, Description: "Enable metrics-server (kubectl top, HPA). Default true."},
					"ingress_nginx":       schema.BoolAttribute{Optional: true, Computed: true, Description: "Install ingress-nginx controller. Default false."},
					"cert_manager":        schema.BoolAttribute{Optional: true, Computed: true, Description: "Install cert-manager (TLS automation). Default false."},
					"kyverno":             schema.BoolAttribute{Optional: true, Computed: true, Description: "Install Kyverno policy engine. Default false."},
					"cilium":              schema.BoolAttribute{Optional: true, Computed: true, Description: "Use Cilium CNI (overrides calico/flannel). Default false."},
					"local_path_storage":  schema.BoolAttribute{Optional: true, Computed: true, Description: "Install local-path-provisioner (default StorageClass)."},
					"velero":              schema.BoolAttribute{Optional: true, Computed: true, Description: "Install Velero CLI + namespace for backup/restore."},
					"irsa":                schema.BoolAttribute{Optional: true, Computed: true, Description: "Configure cluster as OIDC issuer for IAM Roles for Service Accounts. Default true."},
					"longhorn":            schema.BoolAttribute{Optional: true, Computed: true, Description: "Install Longhorn distributed block storage (default StorageClass when on)."},
					"default_deny_netpol": schema.BoolAttribute{Optional: true, Computed: true, Description: "Apply baseline NetworkPolicy + Calico GlobalNetworkPolicy that deny cross-namespace traffic by default. Toggling on later will NOT retroactively install — must be set at create time. Default false."},
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
			"oidc_issuer": schema.StringAttribute{
				Description: "OIDC issuer URL for IRSA. Configure projected ServiceAccount tokens with audience=sts.kubmix.cloud against this issuer.",
				Computed:    true,
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

// buildAddonsBody converts the TF addons object into the backend's camelCase
// JSON map. Returns nil if the object is null/unknown so callers can skip it.
func buildAddonsBody(obj types.Object) map[string]interface{} {
	if obj.IsNull() || obj.IsUnknown() {
		return nil
	}
	addonsMap := obj.Attributes()
	out := map[string]interface{}{}
	for tfKey, apiKey := range addonsBackendKey {
		v, ok := addonsMap[tfKey]
		if !ok || v.IsNull() || v.IsUnknown() {
			continue
		}
		if b, ok := v.(types.Bool); ok {
			out[apiKey] = b.ValueBool()
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stringListToSlice extracts a []string from a types.List, ignoring null/unknown
// elements. Returns nil if the list itself is null/unknown.
func stringListToSlice(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []string
	l.ElementsAs(ctx, &out, false)
	return out
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

	if !plan.VPCID.IsNull() && !plan.VPCID.IsUnknown() {
		body["vpcId"] = plan.VPCID.ValueString()
	}
	if !plan.MaxPods.IsNull() && !plan.MaxPods.IsUnknown() {
		body["maxPods"] = plan.MaxPods.ValueInt64()
	}
	if !plan.CNI.IsNull() && !plan.CNI.IsUnknown() {
		body["cni"] = plan.CNI.ValueString()
	}
	if !plan.HAControlPlane.IsNull() && !plan.HAControlPlane.IsUnknown() {
		body["haControlPlane"] = plan.HAControlPlane.ValueBool()
	}
	if !plan.BackupBucketID.IsNull() && !plan.BackupBucketID.IsUnknown() {
		body["backupBucketId"] = plan.BackupBucketID.ValueString()
	}
	if !plan.BackupRetention.IsNull() && !plan.BackupRetention.IsUnknown() {
		body["backupRetention"] = plan.BackupRetention.ValueInt64()
	}
	if cidrs := stringListToSlice(ctx, plan.AllowedSourceCIDRs); cidrs != nil {
		body["allowedSourceCidrs"] = cidrs
	}
	if addonsBody := buildAddonsBody(plan.Addons); addonsBody != nil {
		body["addons"] = addonsBody
	}
	if !plan.AutoscalerEnabled.IsNull() && !plan.AutoscalerEnabled.IsUnknown() {
		body["autoscalerEnabled"] = plan.AutoscalerEnabled.ValueBool()
	}
	if !plan.AutoscalerMinNodes.IsNull() && !plan.AutoscalerMinNodes.IsUnknown() {
		body["autoscalerMinNodes"] = plan.AutoscalerMinNodes.ValueInt64()
	}
	if !plan.AutoscalerMaxNodes.IsNull() && !plan.AutoscalerMaxNodes.IsUnknown() {
		body["autoscalerMaxNodes"] = plan.AutoscalerMaxNodes.ValueInt64()
	}
	if !plan.AutohealEnabled.IsNull() && !plan.AutohealEnabled.IsUnknown() {
		body["autohealEnabled"] = plan.AutohealEnabled.ValueBool()
	}

	respBody, statusCode, err := r.client.Post("/kubernetes/clusters", body)
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
	// Synthesize the OIDC issuer from the cluster ID (always known after create).
	plan.OIDCIssuer = types.StringValue(fmt.Sprintf("https://cloud-api.kubmix.com/api/oidc/cluster/%s", plan.ID.ValueString()))
	// Echo back the values we sent (so plan/state line up after first apply).
	if plan.MaxPods.IsNull() || plan.MaxPods.IsUnknown() {
		plan.MaxPods = types.Int64Value(110)
	}
	if plan.CNI.IsNull() || plan.CNI.IsUnknown() {
		plan.CNI = types.StringValue("calico")
	}
	if plan.AllowedSourceCIDRs.IsNull() || plan.AllowedSourceCIDRs.IsUnknown() {
		l, _ := types.ListValue(types.StringType, []attr.Value{types.StringValue("0.0.0.0/0")})
		plan.AllowedSourceCIDRs = l
	}
	if plan.Addons.IsNull() || plan.Addons.IsUnknown() {
		emptyAddons, _ := types.ObjectValue(addonsAttrTypes, map[string]attr.Value{
			"metrics_server":      types.BoolValue(true),
			"ingress_nginx":       types.BoolValue(false),
			"cert_manager":        types.BoolValue(false),
			"kyverno":             types.BoolValue(false),
			"cilium":              types.BoolValue(false),
			"local_path_storage":  types.BoolValue(true),
			"velero":              types.BoolValue(false),
			"irsa":                types.BoolValue(true),
			"longhorn":            types.BoolValue(false),
			"default_deny_netpol": types.BoolValue(false),
		})
		plan.Addons = emptyAddons
	}
	// Autoscaler defaults — backend won't always echo these in the create response,
	// so settle Computed values now to keep plan/state aligned on first apply.
	if v, ok := result["autoscalerEnabled"].(bool); ok {
		plan.AutoscalerEnabled = types.BoolValue(v)
	} else if plan.AutoscalerEnabled.IsNull() || plan.AutoscalerEnabled.IsUnknown() {
		plan.AutoscalerEnabled = types.BoolValue(false)
	}
	if v := getInt64(result, "autoscalerMinNodes"); v > 0 {
		plan.AutoscalerMinNodes = types.Int64Value(v)
	} else if plan.AutoscalerMinNodes.IsNull() || plan.AutoscalerMinNodes.IsUnknown() {
		plan.AutoscalerMinNodes = types.Int64Value(1)
	}
	if v := getInt64(result, "autoscalerMaxNodes"); v > 0 {
		plan.AutoscalerMaxNodes = types.Int64Value(v)
	} else if plan.AutoscalerMaxNodes.IsNull() || plan.AutoscalerMaxNodes.IsUnknown() {
		plan.AutoscalerMaxNodes = types.Int64Value(10)
	}
	if v, ok := result["autohealEnabled"].(bool); ok {
		plan.AutohealEnabled = types.BoolValue(v)
	} else if plan.AutohealEnabled.IsNull() || plan.AutohealEnabled.IsUnknown() {
		plan.AutohealEnabled = types.BoolValue(true)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *K8sClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state K8sClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/kubernetes/clusters/%s", state.ID.ValueString()))
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
	state.OIDCIssuer = types.StringValue(fmt.Sprintf("https://cloud-api.kubmix.com/api/oidc/cluster/%s", state.ID.ValueString()))

	if v := getString(result, "vpcId"); v != "" {
		state.VPCID = types.StringValue(v)
	}

	// HA control plane (immutable after create, so just mirror what the API says).
	if v, ok := result["haControlPlane"].(bool); ok {
		state.HAControlPlane = types.BoolValue(v)
	}

	// Backup bucket (nullable on the backend — if absent/null, set state to null
	// so plan diff is clean for users who didn't configure it).
	if v, ok := result["backupBucketId"].(string); ok && v != "" {
		state.BackupBucketID = types.StringValue(v)
	} else {
		state.BackupBucketID = types.StringNull()
	}

	if v := getInt64(result, "backupRetention"); v > 0 {
		state.BackupRetention = types.Int64Value(v)
	}

	// allowed_source_cidrs is stored as JSON array on the backend.
	if raw, ok := result["allowedSourceCidrs"].([]interface{}); ok {
		vals := make([]attr.Value, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				vals = append(vals, types.StringValue(s))
			}
		}
		l, diags := types.ListValue(types.StringType, vals)
		resp.Diagnostics.Append(diags...)
		state.AllowedSourceCIDRs = l
	}

	// Addons block: read whatever the backend returned, fall back to current
	// state value for any missing key so we don't drift on partial responses.
	if rawAddons, ok := result["addons"].(map[string]interface{}); ok {
		current := map[string]attr.Value{}
		if !state.Addons.IsNull() && !state.Addons.IsUnknown() {
			current = state.Addons.Attributes()
		}
		newAddons := map[string]attr.Value{}
		for tfKey, apiKey := range addonsBackendKey {
			if v, exists := rawAddons[apiKey]; exists {
				if b, ok := v.(bool); ok {
					newAddons[tfKey] = types.BoolValue(b)
					continue
				}
			}
			if cur, exists := current[tfKey]; exists {
				newAddons[tfKey] = cur
			} else {
				newAddons[tfKey] = types.BoolValue(false)
			}
		}
		obj, diags := types.ObjectValue(addonsAttrTypes, newAddons)
		resp.Diagnostics.Append(diags...)
		state.Addons = obj
	}

	if v := getInt64(result, "maxPods"); v > 0 {
		state.MaxPods = types.Int64Value(v)
	}
	if v := getString(result, "cni"); v != "" {
		state.CNI = types.StringValue(v)
	}

	// Autoscaler settings (mutable; backend always returns these once persisted).
	if v, ok := result["autoscalerEnabled"].(bool); ok {
		state.AutoscalerEnabled = types.BoolValue(v)
	} else if state.AutoscalerEnabled.IsNull() || state.AutoscalerEnabled.IsUnknown() {
		state.AutoscalerEnabled = types.BoolValue(false)
	}
	if v := getInt64(result, "autoscalerMinNodes"); v > 0 {
		state.AutoscalerMinNodes = types.Int64Value(v)
	} else if state.AutoscalerMinNodes.IsNull() || state.AutoscalerMinNodes.IsUnknown() {
		state.AutoscalerMinNodes = types.Int64Value(1)
	}
	if v := getInt64(result, "autoscalerMaxNodes"); v > 0 {
		state.AutoscalerMaxNodes = types.Int64Value(v)
	} else if state.AutoscalerMaxNodes.IsNull() || state.AutoscalerMaxNodes.IsUnknown() {
		state.AutoscalerMaxNodes = types.Int64Value(10)
	}
	if v, ok := result["autohealEnabled"].(bool); ok {
		state.AutohealEnabled = types.BoolValue(v)
	} else if state.AutohealEnabled.IsNull() || state.AutohealEnabled.IsUnknown() {
		state.AutohealEnabled = types.BoolValue(true)
	}

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

// boolValueChanged reports whether two types.Bool values represent different
// known booleans. Unknown/null on either side counts as "no change" — callers
// should already have refreshed state via a prior Read.
func boolValueChanged(planVal, stateVal types.Bool) bool {
	if planVal.IsNull() || planVal.IsUnknown() {
		return false
	}
	if stateVal.IsNull() || stateVal.IsUnknown() {
		return true
	}
	return planVal.ValueBool() != stateVal.ValueBool()
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

	// Build PATCH body with only the mutable fields whose plan value differs
	// from current state. The backend's updateClusterSchema accepts:
	//   backupBucketId, backupRetention, addons{...}, allowedSourceCidrs.
	// Other fields (name, version, node_groups, ha_control_plane, cni, region,
	// vpc_id) are either RequiresReplace or not patchable here.
	body := map[string]interface{}{}

	// backup_bucket_id — string, nullable. Send null when the user removes it
	// so the backend clears the column.
	if !plan.BackupBucketID.Equal(state.BackupBucketID) {
		if plan.BackupBucketID.IsNull() || plan.BackupBucketID.IsUnknown() {
			body["backupBucketId"] = nil
		} else {
			body["backupBucketId"] = plan.BackupBucketID.ValueString()
		}
	}

	// backup_retention — int.
	if !plan.BackupRetention.IsNull() && !plan.BackupRetention.IsUnknown() &&
		!plan.BackupRetention.Equal(state.BackupRetention) {
		body["backupRetention"] = plan.BackupRetention.ValueInt64()
	}

	// allowed_source_cidrs — list of string. Send the full new list when changed.
	if !plan.AllowedSourceCIDRs.Equal(state.AllowedSourceCIDRs) {
		if cidrs := stringListToSlice(ctx, plan.AllowedSourceCIDRs); cidrs != nil {
			body["allowedSourceCidrs"] = cidrs
		}
	}

	// autoscaler_enabled — bool, always send when changed.
	if !plan.AutoscalerEnabled.IsNull() && !plan.AutoscalerEnabled.IsUnknown() &&
		!plan.AutoscalerEnabled.Equal(state.AutoscalerEnabled) {
		body["autoscalerEnabled"] = plan.AutoscalerEnabled.ValueBool()
	}
	if !plan.AutoscalerMinNodes.IsNull() && !plan.AutoscalerMinNodes.IsUnknown() &&
		!plan.AutoscalerMinNodes.Equal(state.AutoscalerMinNodes) {
		body["autoscalerMinNodes"] = plan.AutoscalerMinNodes.ValueInt64()
	}
	if !plan.AutoscalerMaxNodes.IsNull() && !plan.AutoscalerMaxNodes.IsUnknown() &&
		!plan.AutoscalerMaxNodes.Equal(state.AutoscalerMaxNodes) {
		body["autoscalerMaxNodes"] = plan.AutoscalerMaxNodes.ValueInt64()
	}
	// autoheal_enabled — bool, send when changed.
	if !plan.AutohealEnabled.IsNull() && !plan.AutohealEnabled.IsUnknown() &&
		!plan.AutohealEnabled.Equal(state.AutohealEnabled) {
		body["autohealEnabled"] = plan.AutohealEnabled.ValueBool()
	}

	// addons — diff key-by-key, only send the ones that changed.
	if !plan.Addons.Equal(state.Addons) {
		planAttrs := map[string]attr.Value{}
		stateAttrs := map[string]attr.Value{}
		if !plan.Addons.IsNull() && !plan.Addons.IsUnknown() {
			planAttrs = plan.Addons.Attributes()
		}
		if !state.Addons.IsNull() && !state.Addons.IsUnknown() {
			stateAttrs = state.Addons.Attributes()
		}
		changed := map[string]interface{}{}
		for tfKey, apiKey := range addonsBackendKey {
			pv, _ := planAttrs[tfKey].(types.Bool)
			sv, _ := stateAttrs[tfKey].(types.Bool)
			if boolValueChanged(pv, sv) {
				changed[apiKey] = pv.ValueBool()
			}
		}
		if len(changed) > 0 {
			body["addons"] = changed
		}
	}

	// If nothing changed in the mutable subset, just persist the plan as new
	// state and exit — Terraform may have triggered Update for a computed-only
	// drift that we already settled via Read.
	if len(body) > 0 {
		_, statusCode, err := r.client.Patch(fmt.Sprintf("/kubernetes/clusters/%s", state.ID.ValueString()), body)
		if err != nil {
			resp.Diagnostics.AddError("Error updating K8s cluster", err.Error())
			return
		}
		if statusCode < 200 || statusCode >= 300 {
			resp.Diagnostics.AddError("API error updating K8s cluster",
				fmt.Sprintf("Status %d", statusCode))
			return
		}
	}

	plan.ID = state.ID
	// Carry forward computed fields from prior state — the PATCH endpoint does
	// not re-issue endpoint/CA, and HAControlPlane is immutable.
	plan.Status = state.Status
	plan.Endpoint = state.Endpoint
	plan.CACert = state.CACert
	plan.OIDCIssuer = state.OIDCIssuer
	plan.HAControlPlane = state.HAControlPlane

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *K8sClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state K8sClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/kubernetes/clusters/%s", state.ID.ValueString()))
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

// --- Validators ---

// retentionRangeValidator enforces 1 <= backup_retention <= 90 (matches backend).
type retentionRangeValidator struct{}

func (retentionRangeValidator) Description(_ context.Context) string {
	return "backup_retention must be between 1 and 90 days"
}
func (retentionRangeValidator) MarkdownDescription(ctx context.Context) string {
	return retentionRangeValidator{}.Description(ctx)
}
func (retentionRangeValidator) ValidateInt64(_ context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	v := req.ConfigValue.ValueInt64()
	if v < 1 || v > 90 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid backup_retention",
			fmt.Sprintf("backup_retention must be between 1 and 90, got %d", v),
		)
	}
}

// nonEmptyStringListValidator ensures the list, if set, has at least one element.
type nonEmptyStringListValidator struct{}

func (nonEmptyStringListValidator) Description(_ context.Context) string {
	return "list must contain at least one entry when set"
}
func (nonEmptyStringListValidator) MarkdownDescription(ctx context.Context) string {
	return nonEmptyStringListValidator{}.Description(ctx)
}
func (nonEmptyStringListValidator) ValidateList(_ context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if len(req.ConfigValue.Elements()) == 0 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"List must not be empty",
			"allowed_source_cidrs must contain at least one CIDR when set; omit the attribute to use the default [\"0.0.0.0/0\"].",
		)
	}
}

