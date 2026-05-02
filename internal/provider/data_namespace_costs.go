package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &NamespaceCostsDataSource{}
	_ datasource.DataSourceWithConfigure = &NamespaceCostsDataSource{}
)

type NamespaceCostsDataSource struct {
	client *ApiClient
}

type NamespaceCostsDataSourceModel struct {
	ClusterID      types.String  `tfsdk:"cluster_id"`
	From           types.String  `tfsdk:"from"`
	To             types.String  `tfsdk:"to"`
	GroupBy        types.String  `tfsdk:"group_by"`
	LabelKey       types.String  `tfsdk:"label_key"`
	TotalUSD       types.Float64 `tfsdk:"total_usd"`
	NamespaceCount types.Int64   `tfsdk:"namespace_count"`
	Rows           types.List    `tfsdk:"rows"`
}

var namespaceCostRowAttrTypes = map[string]attr.Type{
	"key":               types.StringType,
	"cost_usd":          types.Float64Type,
	"cpu_hours":         types.Float64Type,
	"ram_gb_hours":      types.Float64Type,
	"storage_gb_months": types.Float64Type,
	"pod_count":         types.Int64Type,
	"percentage":        types.Float64Type,
}

func NewNamespaceCostsDataSource() datasource.DataSource {
	return &NamespaceCostsDataSource{}
}

func (d *NamespaceCostsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_namespace_costs"
}

func (d *NamespaceCostsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Per-namespace Kubernetes cost attribution (OpenCost/Kubecost equivalent). " +
			"Returns each namespace's CPU-hours, RAM GB-hours, storage GB-months, pod count and USD cost " +
			"over the chosen period. Group by `namespace` (default) or by an arbitrary pod `label` key " +
			"(e.g. team, app) for chargeback/showback. Omit `cluster_id` to aggregate across every cluster " +
			"in the org.",
		Attributes: map[string]schema.Attribute{
			"cluster_id": schema.StringAttribute{
				Description: "Optional cluster id. If omitted, costs are aggregated across every Kubernetes cluster in the organization.",
				Optional:    true,
			},
			"from": schema.StringAttribute{
				Description: "Period start (RFC3339 / ISO-8601). Defaults to 30 days ago.",
				Optional:    true,
			},
			"to": schema.StringAttribute{
				Description: "Period end (RFC3339 / ISO-8601). Defaults to now.",
				Optional:    true,
			},
			"group_by": schema.StringAttribute{
				Description: "How to group the rows: \"namespace\" (default) or \"label\".",
				Optional:    true,
			},
			"label_key": schema.StringAttribute{
				Description: "Label key to group by when group_by=\"label\" (e.g. \"team\", \"app\"). Required when group_by=\"label\".",
				Optional:    true,
			},
			"total_usd": schema.Float64Attribute{
				Description: "Total USD cost across all returned rows for the period.",
				Computed:    true,
			},
			"namespace_count": schema.Int64Attribute{
				Description: "Number of distinct namespaces (or label values) in the result.",
				Computed:    true,
			},
			"rows": schema.ListNestedAttribute{
				Description: "Per-namespace (or per-label-value) cost rows.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							Description: "Namespace name (or label value when group_by=label).",
							Computed:    true,
						},
						"cost_usd": schema.Float64Attribute{
							Description: "Total USD cost for this row over the period.",
							Computed:    true,
						},
						"cpu_hours": schema.Float64Attribute{
							Description: "Aggregate CPU-hours consumed.",
							Computed:    true,
						},
						"ram_gb_hours": schema.Float64Attribute{
							Description: "Aggregate RAM GB-hours consumed.",
							Computed:    true,
						},
						"storage_gb_months": schema.Float64Attribute{
							Description: "Aggregate persistent storage GB-months consumed.",
							Computed:    true,
						},
						"pod_count": schema.Int64Attribute{
							Description: "Distinct pod count observed in this row over the period.",
							Computed:    true,
						},
						"percentage": schema.Float64Attribute{
							Description: "Share of total cost (0-100) attributable to this row.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *NamespaceCostsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*ApiClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type",
			"Expected *ApiClient, got something else.")
		return
	}
	d.client = client
}

func (d *NamespaceCostsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config NamespaceCostsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupBy := "namespace"
	if !config.GroupBy.IsNull() && !config.GroupBy.IsUnknown() && config.GroupBy.ValueString() != "" {
		groupBy = config.GroupBy.ValueString()
	}
	if groupBy == "label" {
		if config.LabelKey.IsNull() || config.LabelKey.IsUnknown() || config.LabelKey.ValueString() == "" {
			resp.Diagnostics.AddError(
				"Missing label_key",
				"`label_key` is required when `group_by` is set to \"label\".",
			)
			return
		}
	}

	q := url.Values{}
	if !config.From.IsNull() && !config.From.IsUnknown() && config.From.ValueString() != "" {
		q.Set("from", config.From.ValueString())
	}
	if !config.To.IsNull() && !config.To.IsUnknown() && config.To.ValueString() != "" {
		q.Set("to", config.To.ValueString())
	}
	q.Set("groupBy", groupBy)
	if groupBy == "label" {
		q.Set("labelKey", config.LabelKey.ValueString())
	}

	var path string
	if !config.ClusterID.IsNull() && !config.ClusterID.IsUnknown() && config.ClusterID.ValueString() != "" {
		path = fmt.Sprintf("/kubernetes/clusters/%s/namespace-costs", config.ClusterID.ValueString())
	} else {
		path = "/kubernetes/namespace-costs"
	}
	if encoded := q.Encode(); encoded != "" {
		path = path + "?" + encoded
	}

	respBody, statusCode, err := d.client.Get(path)
	if err != nil {
		resp.Diagnostics.AddError("Error fetching namespace costs", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error fetching namespace costs",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		resp.Diagnostics.AddError("Error parsing namespace-costs response", err.Error())
		return
	}
	// Unwrap a `data` envelope if present.
	if inner, ok := raw["data"].(map[string]interface{}); ok {
		raw = inner
	}

	// Summary.
	var totalUsd float64
	var nsCount int64
	if summary, ok := raw["summary"].(map[string]interface{}); ok {
		totalUsd = getFloat64(summary, "totalUsd")
		if totalUsd == 0 {
			totalUsd = getFloat64(summary, "total_usd")
		}
		nsCount = getInt64(summary, "namespaceCount")
		if nsCount == 0 {
			nsCount = getInt64(summary, "namespace_count")
		}
	}

	// Rows.
	var rawRows []interface{}
	if rs, ok := raw["rows"].([]interface{}); ok {
		rawRows = rs
	}

	rowValues := make([]attr.Value, 0, len(rawRows))
	for _, item := range rawRows {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		costUsd := getFloat64(row, "costUsd")
		if costUsd == 0 {
			costUsd = getFloat64(row, "cost_usd")
		}
		cpuHours := getFloat64(row, "cpuHours")
		if cpuHours == 0 {
			cpuHours = getFloat64(row, "cpu_hours")
		}
		ramGbHours := getFloat64(row, "ramGbHours")
		if ramGbHours == 0 {
			ramGbHours = getFloat64(row, "ram_gb_hours")
		}
		storageGbMonths := getFloat64(row, "storageGbMonths")
		if storageGbMonths == 0 {
			storageGbMonths = getFloat64(row, "storage_gb_months")
		}
		podCount := getInt64(row, "podCount")
		if podCount == 0 {
			podCount = getInt64(row, "pod_count")
		}
		percentage := getFloat64(row, "percentage")

		obj, diags := types.ObjectValue(namespaceCostRowAttrTypes, map[string]attr.Value{
			"key":               types.StringValue(getString(row, "key")),
			"cost_usd":          types.Float64Value(costUsd),
			"cpu_hours":         types.Float64Value(cpuHours),
			"ram_gb_hours":      types.Float64Value(ramGbHours),
			"storage_gb_months": types.Float64Value(storageGbMonths),
			"pod_count":         types.Int64Value(podCount),
			"percentage":        types.Float64Value(percentage),
		})
		resp.Diagnostics.Append(diags...)
		rowValues = append(rowValues, obj)
	}

	rowList, diags := types.ListValue(types.ObjectType{AttrTypes: namespaceCostRowAttrTypes}, rowValues)
	resp.Diagnostics.Append(diags...)

	config.TotalUSD = types.Float64Value(totalUsd)
	config.NamespaceCount = types.Int64Value(nsCount)
	config.Rows = rowList

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
