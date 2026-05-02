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
	_ datasource.DataSource              = &OptimizationSavingsDataSource{}
	_ datasource.DataSourceWithConfigure = &OptimizationSavingsDataSource{}
)

type OptimizationSavingsDataSource struct {
	client *ApiClient
}

type OptimizationSavingsDataSourceModel struct {
	ClusterID           types.String  `tfsdk:"cluster_id"`
	PotentialMonthlyUSD types.Float64 `tfsdk:"potential_monthly_usd"`
	AppliedMonthlyUSD   types.Float64 `tfsdk:"applied_monthly_usd"`
	RecommendationCount types.Int64   `tfsdk:"recommendation_count"`
	ByType              types.Map     `tfsdk:"by_type"`
}

func NewOptimizationSavingsDataSource() datasource.DataSource {
	return &OptimizationSavingsDataSource{}
}

func (d *OptimizationSavingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_optimization_savings"
}

func (d *OptimizationSavingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Aggregated DevskinCloud optimization savings summary. Returns potential and applied monthly savings " +
			"plus a per-type breakdown (rightsizing, binpack, ...). Useful for alerting/SLOs around cost optimization.",
		Attributes: map[string]schema.Attribute{
			"cluster_id": schema.StringAttribute{
				Description: "Optionally restrict the savings summary to a single Kubernetes cluster.",
				Optional:    true,
			},
			"potential_monthly_usd": schema.Float64Attribute{
				Description: "Total monthly savings (USD) projected if all open recommendations are applied.",
				Computed:    true,
			},
			"applied_monthly_usd": schema.Float64Attribute{
				Description: "Total monthly savings (USD) already realized from applied recommendations.",
				Computed:    true,
			},
			"recommendation_count": schema.Int64Attribute{
				Description: "Number of recommendations included in the summary.",
				Computed:    true,
			},
			"by_type": schema.MapAttribute{
				Description: "Per-type breakdown of potential monthly savings (e.g. {rightsizing = 123.45, binpack = 67.89}).",
				ElementType: types.Float64Type,
				Computed:    true,
			},
		},
	}
}

func (d *OptimizationSavingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *OptimizationSavingsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config OptimizationSavingsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	q := url.Values{}
	if !config.ClusterID.IsNull() && !config.ClusterID.IsUnknown() {
		q.Set("clusterId", config.ClusterID.ValueString())
	}

	path := "/optimization/savings"
	if encoded := q.Encode(); encoded != "" {
		path = path + "?" + encoded
	}

	respBody, statusCode, err := d.client.Get(path)
	if err != nil {
		resp.Diagnostics.AddError("Error fetching optimization savings", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error fetching optimization savings",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing savings response", err.Error())
		return
	}
	// Unwrap a `data` key if the backend wraps the payload.
	if inner, ok := result["data"].(map[string]interface{}); ok {
		result = inner
	}

	potential := getFloat64(result, "potential_monthly_usd")
	if potential == 0 {
		potential = getFloat64(result, "potentialMonthlyUsd")
	}
	applied := getFloat64(result, "applied_monthly_usd")
	if applied == 0 {
		applied = getFloat64(result, "appliedMonthlyUsd")
	}
	count := getInt64(result, "recommendation_count")
	if count == 0 {
		count = getInt64(result, "recommendationCount")
	}

	byTypeElems := map[string]float64{}
	if raw, ok := result["by_type"].(map[string]interface{}); ok {
		for k, v := range raw {
			byTypeElems[k] = toFloat64(v)
		}
	} else if raw, ok := result["byType"].(map[string]interface{}); ok {
		for k, v := range raw {
			byTypeElems[k] = toFloat64(v)
		}
	}

	mapElements := map[string]attr.Value{}
	for k, v := range byTypeElems {
		mapElements[k] = types.Float64Value(v)
	}
	mv, diags := types.MapValue(types.Float64Type, mapElements)
	resp.Diagnostics.Append(diags...)

	config.PotentialMonthlyUSD = types.Float64Value(potential)
	config.AppliedMonthlyUSD = types.Float64Value(applied)
	config.RecommendationCount = types.Int64Value(count)
	config.ByType = mv

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}

// toFloat64 coerces an arbitrary JSON-decoded number into a float64.
func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
