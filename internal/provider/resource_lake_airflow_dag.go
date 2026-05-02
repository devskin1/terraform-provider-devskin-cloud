package provider

// DEPRECATED — Apache Airflow is no longer a managed feature on the platform.
// It is now deployed as a marketplace VM (mp-050). The /lakehouse/workflows/*
// endpoints were removed; this resource short-circuits every CRUD method with
// a clear error pointing to the new flow.
//
// The schema is preserved so existing state files still parse; it just cannot
// be mutated. Existing `terraform import` / `terraform state rm` flows still
// work to evict the resource from state.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &LakeAirflowDagResource{}
	_ resource.ResourceWithConfigure = &LakeAirflowDagResource{}
)

const lakeAirflowDeprecationMsg = "Apache Airflow is now deployed as a marketplace VM (mp-050) instead of as a managed feature. " +
	"Use the marketplace deploy flow with `mp-050` to spin up Airflow on your own VM, then drop DAG files at " +
	"/opt/airflow/dags/ via SSH. This resource will be removed in a future release."

const lakeAirflowMigrationDetail = "The /api/lakehouse/workflows/dags* endpoints were removed from the platform. " +
	"To migrate:\n" +
	"  1. terraform state rm devskin_lake_airflow_dag.<name>\n" +
	"  2. devskin marketplace deploy mp-050 --name my-airflow\n" +
	"  3. SCP your DAG file to /opt/airflow/dags/ on the new VM."

type LakeAirflowDagResource struct {
	client *ApiClient
}

type LakeAirflowDagResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	DagID        types.String `tfsdk:"dag_id"`
	Description  types.String `tfsdk:"description"`
	ScheduleCron types.String `tfsdk:"schedule_cron"`
	PythonCode   types.String `tfsdk:"python_code"`
	Enabled      types.Bool   `tfsdk:"enabled"`
}

func NewLakeAirflowDagResource() resource.Resource {
	return &LakeAirflowDagResource{}
}

func (r *LakeAirflowDagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_airflow_dag"
}

func (r *LakeAirflowDagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         lakeAirflowDeprecationMsg,
		DeprecationMessage:  lakeAirflowDeprecationMsg,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "Display / dedup name.",
				Required:    true,
			},
			"dag_id": schema.StringAttribute{
				Description:   "Airflow dag_id assigned by the platform.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				Optional: true,
			},
			"schedule_cron": schema.StringAttribute{
				Description: "Cron schedule expression. Empty / null means manual.",
				Optional:    true,
			},
			"python_code": schema.StringAttribute{
				Description: "Python source for the DAG file.",
				Required:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the DAG is unpaused / scheduled.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
		},
	}
}

func (r *LakeAirflowDagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeAirflowDagResource) Create(_ context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError(
		"devskin_lake_airflow_dag is deprecated",
		lakeAirflowDeprecationMsg+"\n\n"+lakeAirflowMigrationDetail,
	)
}

func (r *LakeAirflowDagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Pass state through unchanged — we don't want a no-op read to drop the
	// resource from state, that's the user's call via `state rm`.
	var state LakeAirflowDagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(
		"devskin_lake_airflow_dag is deprecated — Read is a no-op",
		lakeAirflowDeprecationMsg+"\n\n"+lakeAirflowMigrationDetail,
	)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeAirflowDagResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"devskin_lake_airflow_dag is deprecated",
		lakeAirflowDeprecationMsg+"\n\n"+lakeAirflowMigrationDetail,
	)
}

func (r *LakeAirflowDagResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Allow eviction from state without an API call — the endpoint is gone, so
	// there's nothing to delete remotely. The framework removes the resource
	// from state when Delete returns without errors.
	resp.Diagnostics.AddWarning(
		"devskin_lake_airflow_dag is deprecated — removing from state without API call",
		lakeAirflowDeprecationMsg,
	)
}
