package provider

// DEPRECATED — managed Kafka clusters were retired (Kafka now ships as a
// marketplace VM, mp-040). The topic resource cannot operate without the
// managed cluster API, so it is short-circuited.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &LakeKafkaTopicResource{}
	_ resource.ResourceWithConfigure = &LakeKafkaTopicResource{}
)

const lakeKafkaTopicDeprecationMsg = "Kafka topics are no longer managed via the platform. Apache Kafka now runs on a marketplace VM (mp-040) — create topics with `kafka-topics.sh --create` from inside or against the VM."

const lakeKafkaTopicMigrationDetail = "Inside the Kafka VM (or any host with kafka-topics.sh):\n" +
	"  /opt/kafka/bin/kafka-topics.sh --create \\\n" +
	"    --bootstrap-server <vm-public-ip>:9092 \\\n" +
	"    --topic <name> --partitions <n> --replication-factor 1\n" +
	"Then `terraform state rm devskin_lake_kafka_topic.<name>`."

type LakeKafkaTopicResource struct {
	client *ApiClient
}

type LakeKafkaTopicResourceModel struct {
	ID          types.String `tfsdk:"id"`
	ClusterID   types.String `tfsdk:"cluster_id"`
	Name        types.String `tfsdk:"name"`
	Partitions  types.Int64  `tfsdk:"partitions"`
	Replication types.Int64  `tfsdk:"replication"`
}

func NewLakeKafkaTopicResource() resource.Resource {
	return &LakeKafkaTopicResource{}
}

func (r *LakeKafkaTopicResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_kafka_topic"
}

func (r *LakeKafkaTopicResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:        lakeKafkaTopicDeprecationMsg,
		DeprecationMessage: lakeKafkaTopicDeprecationMsg,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cluster_id": schema.StringAttribute{
				Description:   "Parent kafka cluster id.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Description:   "Topic name.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"partitions": schema.Int64Attribute{
				Description: "Number of partitions.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(3),
			},
			"replication": schema.Int64Attribute{
				Description: "Replication factor.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(2),
			},
		},
	}
}

func (r *LakeKafkaTopicResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeKafkaTopicResource) Create(_ context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError(
		"devskin_lake_kafka_topic is deprecated",
		lakeKafkaTopicDeprecationMsg+"\n\n"+lakeKafkaTopicMigrationDetail,
	)
}

func (r *LakeKafkaTopicResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeKafkaTopicResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(
		"devskin_lake_kafka_topic is deprecated — Read is a no-op",
		lakeKafkaTopicDeprecationMsg+"\n\n"+lakeKafkaTopicMigrationDetail,
	)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeKafkaTopicResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"devskin_lake_kafka_topic is deprecated",
		lakeKafkaTopicDeprecationMsg+"\n\n"+lakeKafkaTopicMigrationDetail,
	)
}

func (r *LakeKafkaTopicResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"devskin_lake_kafka_topic is deprecated — removing from state without API call",
		lakeKafkaTopicDeprecationMsg,
	)
}
