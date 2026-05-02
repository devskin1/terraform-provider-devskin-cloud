package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &LakeSparkJobResource{}
	_ resource.ResourceWithConfigure = &LakeSparkJobResource{}
)

type LakeSparkJobResource struct {
	client *ApiClient
}

type LakeSparkJobResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Language         types.String `tfsdk:"language"`
	Code             types.String `tfsdk:"code"`
	JarS3Key         types.String `tfsdk:"jar_s3_key"`
	DriverCores      types.Int64  `tfsdk:"driver_cores"`
	DriverMemoryGB   types.Int64  `tfsdk:"driver_memory_gb"`
	ExecutorCores    types.Int64  `tfsdk:"executor_cores"`
	ExecutorMemoryGB types.Int64  `tfsdk:"executor_memory_gb"`
	NumExecutors     types.Int64  `tfsdk:"num_executors"`
	MaxExecutors     types.Int64  `tfsdk:"max_executors"`
	RetryCount       types.Int64  `tfsdk:"retry_count"`
	TimeoutMin       types.Int64  `tfsdk:"timeout_min"`
	AlertEmail       types.String `tfsdk:"alert_email"`
	ScheduleCron     types.String `tfsdk:"schedule_cron"`
}

func NewLakeSparkJobResource() resource.Resource {
	return &LakeSparkJobResource{}
}

func (r *LakeSparkJobResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lake_spark_job"
}

func (r *LakeSparkJobResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinLake Spark job definition (PySpark / Scala / SparkSQL). Mutable scalars (cores, memory, schedule) update in place; name and language require replace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description:   "Spark job name.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"language": schema.StringAttribute{
				Description:   "Job language. One of: PYSPARK, SCALA, SQL.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"code": schema.StringAttribute{
				Description: "Inline source for PYSPARK / SQL jobs. Mutually exclusive with jar_s3_key.",
				Optional:    true,
			},
			"jar_s3_key": schema.StringAttribute{
				Description: "S3 key of the uploaded JAR for SCALA jobs.",
				Optional:    true,
			},
			"driver_cores": schema.Int64Attribute{
				Description: "Driver CPU cores.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
			},
			"driver_memory_gb": schema.Int64Attribute{
				Description: "Driver memory in GB.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(2),
			},
			"executor_cores": schema.Int64Attribute{
				Description: "Cores per executor.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(2),
			},
			"executor_memory_gb": schema.Int64Attribute{
				Description: "Memory per executor in GB.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(4),
			},
			"num_executors": schema.Int64Attribute{
				Description: "Executor count.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(2),
			},
			"max_executors": schema.Int64Attribute{
				Description: "Maximum executors when dynamic allocation is enabled. 0 = auto/disabled.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
			},
			"retry_count": schema.Int64Attribute{
				Description: "Number of automatic retries on failure. 0 = no retry.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
			},
			"timeout_min": schema.Int64Attribute{
				Description: "Per-run timeout in minutes. 0 = no timeout.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
			},
			"alert_email": schema.StringAttribute{
				Description: "Optional email address to notify on failure.",
				Optional:    true,
			},
			"schedule_cron": schema.StringAttribute{
				Description: "Optional cron expression for scheduled runs.",
				Optional:    true,
			},
		},
	}
}

func (r *LakeSparkJobResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LakeSparkJobResource) buildBody(plan LakeSparkJobResourceModel) map[string]interface{} {
	body := map[string]interface{}{
		"name":             plan.Name.ValueString(),
		"language":         plan.Language.ValueString(),
		"driverCores":      plan.DriverCores.ValueInt64(),
		"driverMemoryGb":   plan.DriverMemoryGB.ValueInt64(),
		"executorCores":    plan.ExecutorCores.ValueInt64(),
		"executorMemoryGb": plan.ExecutorMemoryGB.ValueInt64(),
		"numExecutors":     plan.NumExecutors.ValueInt64(),
		"maxExecutors":     plan.MaxExecutors.ValueInt64(),
		"retryCount":       plan.RetryCount.ValueInt64(),
		"timeoutMin":       plan.TimeoutMin.ValueInt64(),
	}
	if !plan.Code.IsNull() && !plan.Code.IsUnknown() {
		body["code"] = plan.Code.ValueString()
	}
	if !plan.JarS3Key.IsNull() && !plan.JarS3Key.IsUnknown() {
		body["jarS3Key"] = plan.JarS3Key.ValueString()
	}
	if !plan.AlertEmail.IsNull() && !plan.AlertEmail.IsUnknown() {
		body["alertEmail"] = plan.AlertEmail.ValueString()
	}
	if !plan.ScheduleCron.IsNull() && !plan.ScheduleCron.IsUnknown() {
		body["scheduleCron"] = plan.ScheduleCron.ValueString()
	}
	return body
}

func (r *LakeSparkJobResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan LakeSparkJobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Post("/lakehouse/spark/jobs", r.buildBody(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating spark job", err.Error())
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error creating spark job",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}
	plan.ID = types.StringValue(getString(result, "id"))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeSparkJobResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state LakeSparkJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Get(fmt.Sprintf("/lakehouse/spark/jobs/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error reading spark job", err.Error())
		return
	}
	if statusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		resp.Diagnostics.AddError("API error reading spark job",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		resp.Diagnostics.AddError("Error parsing response", err.Error())
		return
	}

	state.Name = types.StringValue(getString(result, "name"))
	state.Language = types.StringValue(getString(result, "language"))
	if v := getString(result, "code"); v != "" {
		state.Code = types.StringValue(v)
	}
	if v := getString(result, "jarS3Key"); v != "" {
		state.JarS3Key = types.StringValue(v)
	}
	state.DriverCores = types.Int64Value(getInt64(result, "driverCores"))
	state.DriverMemoryGB = types.Int64Value(getInt64(result, "driverMemoryGb"))
	state.ExecutorCores = types.Int64Value(getInt64(result, "executorCores"))
	state.ExecutorMemoryGB = types.Int64Value(getInt64(result, "executorMemoryGb"))
	state.NumExecutors = types.Int64Value(getInt64(result, "numExecutors"))
	state.MaxExecutors = types.Int64Value(getInt64(result, "maxExecutors"))
	state.RetryCount = types.Int64Value(getInt64(result, "retryCount"))
	state.TimeoutMin = types.Int64Value(getInt64(result, "timeoutMin"))
	if v := getString(result, "alertEmail"); v != "" {
		state.AlertEmail = types.StringValue(v)
	}
	if v := getString(result, "scheduleCron"); v != "" {
		state.ScheduleCron = types.StringValue(v)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *LakeSparkJobResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan LakeSparkJobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state LakeSparkJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// NOTE: backend PATCH handler may not exist yet; we still call it so that
	// once it lands the provider works without changes. Tolerate 404.
	respBody, statusCode, err := r.client.Patch(fmt.Sprintf("/lakehouse/spark/jobs/%s", state.ID.ValueString()), r.buildBody(plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating spark job", err.Error())
		return
	}
	if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
		resp.Diagnostics.AddError("API error updating spark job",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
		return
	}
	if statusCode == 404 {
		resp.Diagnostics.AddWarning(
			"Spark job PATCH endpoint missing",
			"The backend PATCH /api/lakehouse/spark/jobs/:id endpoint is not implemented yet. State updated locally only.",
		)
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *LakeSparkJobResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state LakeSparkJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, statusCode, err := r.client.Delete(fmt.Sprintf("/lakehouse/spark/jobs/%s", state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting spark job", err.Error())
		return
	}
	if statusCode != 404 && (statusCode < 200 || statusCode >= 300) {
		resp.Diagnostics.AddError("API error deleting spark job",
			fmt.Sprintf("Status %d: %s", statusCode, string(respBody)))
	}
}
