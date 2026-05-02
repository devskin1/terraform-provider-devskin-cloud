package provider

// DEPRECATED — managed Kafka clusters were retired. Apache Kafka now ships as
// a marketplace VM (mp-040, tpl-204). The schema is preserved so existing
// state files still parse; CRUD is short-circuited with a clear error.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &LakeKafkaClusterResource{}
	_ resource.ResourceWithConfigure = &LakeKafkaClusterResource{}
)

const lakeKafkaDeprecationMsg = "Apache Kafka is now deployed as a marketplace VM (mp-040) instead of as a managed cluster. " +
	"Use the marketplace deploy flow with `mp-040` to spin up Kafka on your own VM. " +
	"This resource will be removed in a future release."

const lakeKafkaMigrationDetail = "The /api/lakehouse/streaming/clusters* endpoints were retired. To migrate:\n" +
	"  1. terraform state rm devskin_lake_kafka_cluster.<name>\n" +
	"  2. Create a devskin_instance with image_id = \"tpl-204\" (or hit POST /api/marketplace/products/mp-040/deploy)\n" +
	"  3. Connect with bootstrap-server <vm-public-ip>:9092"

type LakeKafkaClusterResource struct {
	client *ApiClient
}

type LakeKafkaClusterResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	BrokerCount    types.Int64  `tfsdk:"broker_count"`
	StorageGB      types.Int64  `tfsdk:"storage_gb"`
	RetentionHours types.Int64  `tfsdk:"retention_hours"`
	BootstrapHost  types.String `tfsdk:"bootstrap_host"`
	Status         types.String `tfsdk:"status"`
}

func NewLakeKafkaClusterResource() resource.Resource {
	return &LakeKafkaClusterResource{}
}

func (r *LakeKafkaClusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_kafka_cluster"
}

func (r *LakeKafkaClusterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:        lakeKafkaDeprecationMsg,
		DeprecationMessage: lakeKafkaDeprecationMsg,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "Cluster name.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"broker_count": schema.Int64Attribute{
				Description:   "Number of broker nodes (1-9).",
				Required:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"storage_gb": schema.Int64Attribute{
				Description:   "Per-broker storage in GB (10-2000).",
				Required:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"retention_hours": schema.Int64Attribute{
				Description: "Default topic retention in hours (default 168 = 7 days).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(168),
			},
			"bootstrap_host": schema.StringAttribute{
				Description: "Bootstrap broker host:port.",
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Cluster status.",
				Computed:    true,
			},
		},
	}
}

func (r *LakeKafkaClusterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeKafkaClusterResource) Create(_ context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError(
		"devskin_lake_kafka_cluster is deprecated",
		lakeKafkaDeprecationMsg+"\n\n"+lakeKafkaMigrationDetail,
	)
}

func (r *LakeKafkaClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeKafkaClusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(
		"devskin_lake_kafka_cluster is deprecated — Read is a no-op",
		lakeKafkaDeprecationMsg+"\n\n"+lakeKafkaMigrationDetail,
	)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeKafkaClusterResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"devskin_lake_kafka_cluster is deprecated",
		lakeKafkaDeprecationMsg+"\n\n"+lakeKafkaMigrationDetail,
	)
}

func (r *LakeKafkaClusterResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"devskin_lake_kafka_cluster is deprecated — removing from state without API call",
		lakeKafkaDeprecationMsg,
	)
}
