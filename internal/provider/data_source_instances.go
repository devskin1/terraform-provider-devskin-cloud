package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &InstancesDataSource{}
	_ datasource.DataSourceWithConfigure = &InstancesDataSource{}
)

type InstancesDataSource struct {
	client *ApiClient
}

type InstancesDataSourceModel struct {
	Region    types.String `tfsdk:"region"`
	Instances types.List   `tfsdk:"instances"`
}

var instanceDataAttrTypes = map[string]attr.Type{
	"id":            types.StringType,
	"name":          types.StringType,
	"instance_type": types.StringType,
	"image_id":      types.StringType,
	"region":        types.StringType,
	"vpc_id":        types.StringType,
	"subnet_id":     types.StringType,
	"status":        types.StringType,
	"public_ip":     types.StringType,
	"private_ip":    types.StringType,
}

func NewInstancesDataSource() datasource.DataSource {
	return &InstancesDataSource{}
}

func (d *InstancesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instances"
}

func (d *InstancesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists DevskinCloud compute instances, optionally filtered by region.",
		Attributes: map[string]schema.Attribute{
			"region": schema.StringAttribute{
				Description: "Filter instances by region. If not specified, all instances are returned.",
				Optional:    true,
			},
			"instances": schema.ListNestedAttribute{
				Description: "The list of instances.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The instance ID.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The instance name.",
							Computed:    true,
						},
						"instance_type": schema.StringAttribute{
							Description: "The instance type.",
							Computed:    true,
						},
						"image_id": schema.StringAttribute{
							Description: "The image ID.",
							Computed:    true,
						},
						"region": schema.StringAttribute{
							Description: "The region.",
							Computed:    true,
						},
						"vpc_id": schema.StringAttribute{
							Description: "The VPC ID.",
							Computed:    true,
						},
						"subnet_id": schema.StringAttribute{
							Description: "The subnet ID.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "The instance status.",
							Computed:    true,
						},
						"public_ip": schema.StringAttribute{
							Description: "The public IP.",
							Computed:    true,
						},
						"private_ip": schema.StringAttribute{
							Description: "The private IP.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *InstancesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *InstancesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config InstancesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	path := "/compute/instances"
	if !config.Region.IsNull() && !config.Region.IsUnknown() {
		path = fmt.Sprintf("/instances?region=%s", config.Region.ValueString())
	}

	respBody, statusCode, err := d.client.Get(path)
	if err != nil {
		resp.Diagnostics.AddError("Error listing instances", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error listing instances",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var rawInstances []map[string]interface{}
	if err := json.Unmarshal(respBody, &rawInstances); err != nil {
		// Try unwrapping from a data key
		var wrapper map[string]json.RawMessage
		if err2 := json.Unmarshal(respBody, &wrapper); err2 == nil {
			if data, ok := wrapper["data"]; ok {
				if err3 := json.Unmarshal(data, &rawInstances); err3 != nil {
					resp.Diagnostics.AddError("Error parsing instances response", err3.Error())
					return
				}
			} else {
				resp.Diagnostics.AddError("Error parsing instances response", err.Error())
				return
			}
		} else {
			resp.Diagnostics.AddError("Error parsing instances response", err.Error())
			return
		}
	}

	instanceValues := make([]attr.Value, len(rawInstances))
	for i, inst := range rawInstances {
		instanceValues[i], _ = types.ObjectValue(instanceDataAttrTypes, map[string]attr.Value{
			"id":            types.StringValue(getString(inst, "id")),
			"name":          types.StringValue(getString(inst, "name")),
			"instance_type": types.StringValue(getString(inst, "instance_type")),
			"image_id":      types.StringValue(getString(inst, "image_id")),
			"region":        types.StringValue(getString(inst, "region")),
			"vpc_id":        types.StringValue(getString(inst, "vpc_id")),
			"subnet_id":     types.StringValue(getString(inst, "subnet_id")),
			"status":        types.StringValue(getString(inst, "status")),
			"public_ip":     types.StringValue(getString(inst, "public_ip")),
			"private_ip":    types.StringValue(getString(inst, "private_ip")),
		})
	}

	instanceList, diags := types.ListValue(types.ObjectType{AttrTypes: instanceDataAttrTypes}, instanceValues)
	resp.Diagnostics.Append(diags...)

	config.Instances = instanceList

	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
