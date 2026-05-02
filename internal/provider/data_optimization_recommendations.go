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
	_ datasource.DataSource              = &OptimizationRecommendationsDataSource{}
	_ datasource.DataSourceWithConfigure = &OptimizationRecommendationsDataSource{}
)

type OptimizationRecommendationsDataSource struct {
	client *ApiClient
}

type OptimizationRecommendationsDataSourceModel struct {
	Status          types.String  `tfsdk:"status"`
	Type            types.String  `tfsdk:"type"`
	ClusterID       types.String  `tfsdk:"cluster_id"`
	Recommendations types.List    `tfsdk:"recommendations"`
	TotalSavingsUSD types.Float64 `tfsdk:"total_savings_usd"`
}

var optimizationRecommendationAttrTypes = map[string]attr.Type{
	"id":                    types.StringType,
	"type":                  types.StringType,
	"severity":              types.StringType,
	"resource_kind":         types.StringType,
	"resource_name":         types.StringType,
	"resource_namespace":    types.StringType,
	"rationale":             types.StringType,
	"estimated_savings_usd": types.Float64Type,
	"confidence":            types.Float64Type,
	"status":                types.StringType,
	"detected_at":           types.StringType,
}

func NewOptimizationRecommendationsDataSource() datasource.DataSource {
	return &OptimizationRecommendationsDataSource{}
}

func (d *OptimizationRecommendationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_optimization_recommendations"
}

func (d *OptimizationRecommendationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists DevskinCloud Kubernetes optimization recommendations (rightsizing, bin-packing, idle workloads). " +
			"Useful for surfacing cost-saving recommendations in dashboards or alerting when the projected monthly savings " +
			"exceed a threshold.",
		Attributes: map[string]schema.Attribute{
			"status": schema.StringAttribute{
				Description: "Filter recommendations by status (e.g. NEW, APPLIED, DISMISSED). Optional.",
				Optional:    true,
			},
			"type": schema.StringAttribute{
				Description: "Filter recommendations by type (e.g. rightsizing, binpack, idle). Optional.",
				Optional:    true,
			},
			"cluster_id": schema.StringAttribute{
				Description: "Filter recommendations to a single cluster. Optional.",
				Optional:    true,
			},
			"total_savings_usd": schema.Float64Attribute{
				Description: "Sum of estimated_savings_usd across the returned recommendations.",
				Computed:    true,
			},
			"recommendations": schema.ListNestedAttribute{
				Description: "The list of optimization recommendations matching the filters.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The recommendation ID.",
							Computed:    true,
						},
						"type": schema.StringAttribute{
							Description: "The recommendation type (rightsizing, binpack, idle).",
							Computed:    true,
						},
						"severity": schema.StringAttribute{
							Description: "Severity (info, low, medium, high).",
							Computed:    true,
						},
						"resource_kind": schema.StringAttribute{
							Description: "Kubernetes resource kind (Deployment, StatefulSet, Pod, Node).",
							Computed:    true,
						},
						"resource_name": schema.StringAttribute{
							Description: "Resource name.",
							Computed:    true,
						},
						"resource_namespace": schema.StringAttribute{
							Description: "Resource namespace.",
							Computed:    true,
						},
						"rationale": schema.StringAttribute{
							Description: "Human-readable explanation of why the recommendation was emitted.",
							Computed:    true,
						},
						"estimated_savings_usd": schema.Float64Attribute{
							Description: "Estimated monthly savings in USD if the recommendation is applied.",
							Computed:    true,
						},
						"confidence": schema.Float64Attribute{
							Description: "Confidence score in [0,1] from the optimizer.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "Current status of the recommendation.",
							Computed:    true,
						},
						"detected_at": schema.StringAttribute{
							Description: "ISO8601 timestamp when the recommendation was detected.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *OptimizationRecommendationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *OptimizationRecommendationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config OptimizationRecommendationsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	q := url.Values{}
	if !config.Status.IsNull() && !config.Status.IsUnknown() {
		q.Set("status", config.Status.ValueString())
	}
	if !config.Type.IsNull() && !config.Type.IsUnknown() {
		q.Set("type", config.Type.ValueString())
	}
	if !config.ClusterID.IsNull() && !config.ClusterID.IsUnknown() {
		q.Set("clusterId", config.ClusterID.ValueString())
	}

	path := "/optimization/recommendations"
	if encoded := q.Encode(); encoded != "" {
		path = path + "?" + encoded
	}

	respBody, statusCode, err := d.client.Get(path)
	if err != nil {
		resp.Diagnostics.AddError("Error listing optimization recommendations", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error listing optimization recommendations",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var rawRecs []map[string]interface{}
	if err := json.Unmarshal(respBody, &rawRecs); err != nil {
		// Try unwrapping from a data key
		var wrapper map[string]json.RawMessage
		if err2 := json.Unmarshal(respBody, &wrapper); err2 == nil {
			if data, ok := wrapper["data"]; ok {
				if err3 := json.Unmarshal(data, &rawRecs); err3 != nil {
					resp.Diagnostics.AddError("Error parsing recommendations response", err3.Error())
					return
				}
			} else if recs, ok := wrapper["recommendations"]; ok {
				if err3 := json.Unmarshal(recs, &rawRecs); err3 != nil {
					resp.Diagnostics.AddError("Error parsing recommendations response", err3.Error())
					return
				}
			} else {
				resp.Diagnostics.AddError("Error parsing recommendations response", err.Error())
				return
			}
		} else {
			resp.Diagnostics.AddError("Error parsing recommendations response", err.Error())
			return
		}
	}

	recValues := make([]attr.Value, len(rawRecs))
	var totalSavings float64
	for i, rec := range rawRecs {
		savings := getFloat64(rec, "estimated_savings_usd")
		if savings == 0 {
			savings = getFloat64(rec, "estimatedSavingsUsd")
		}
		totalSavings += savings

		confidence := getFloat64(rec, "confidence")

		detectedAt := getString(rec, "detected_at")
		if detectedAt == "" {
			detectedAt = getString(rec, "detectedAt")
		}

		resourceKind := getString(rec, "resource_kind")
		if resourceKind == "" {
			resourceKind = getString(rec, "resourceKind")
		}
		resourceName := getString(rec, "resource_name")
		if resourceName == "" {
			resourceName = getString(rec, "resourceName")
		}
		resourceNamespace := getString(rec, "resource_namespace")
		if resourceNamespace == "" {
			resourceNamespace = getString(rec, "resourceNamespace")
		}

		recValues[i], _ = types.ObjectValue(optimizationRecommendationAttrTypes, map[string]attr.Value{
			"id":                    types.StringValue(getString(rec, "id")),
			"type":                  types.StringValue(getString(rec, "type")),
			"severity":              types.StringValue(getString(rec, "severity")),
			"resource_kind":         types.StringValue(resourceKind),
			"resource_name":         types.StringValue(resourceName),
			"resource_namespace":    types.StringValue(resourceNamespace),
			"rationale":             types.StringValue(getString(rec, "rationale")),
			"estimated_savings_usd": types.Float64Value(savings),
			"confidence":            types.Float64Value(confidence),
			"status":                types.StringValue(getString(rec, "status")),
			"detected_at":           types.StringValue(detectedAt),
		})
	}

	recList, diags := types.ListValue(types.ObjectType{AttrTypes: optimizationRecommendationAttrTypes}, recValues)
	resp.Diagnostics.Append(diags...)

	config.Recommendations = recList
	config.TotalSavingsUSD = types.Float64Value(totalSavings)

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
