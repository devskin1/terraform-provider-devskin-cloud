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
	_ resource.Resource              = &AIKnowledgeBaseResource{}
	_ resource.ResourceWithConfigure = &AIKnowledgeBaseResource{}
)

type AIKnowledgeBaseResource struct {
	client *ApiClient
}

type AIKnowledgeBaseResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	EmbeddingModel types.String `tfsdk:"embedding_model"`
	ChatModel      types.String `tfsdk:"chat_model"`
}

func NewAIKnowledgeBaseResource() resource.Resource {
	return &AIKnowledgeBaseResource{}
}

func (r *AIKnowledgeBaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ai_knowledge_base"
}

func (r *AIKnowledgeBaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a DevskinCloud AI Knowledge Base for retrieval-augmented generation (RAG). Documents added to the KB are chunked, embedded, and queryable via the `/ai/knowledge-bases/{id}/query` endpoint.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique KB name within the organization.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Optional purpose / scope description.",
			},
			"embedding_model": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "OpenAI embedding model used for ingestion. One of: `text-embedding-3-small` (default), `text-embedding-3-large`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"chat_model": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Default chat model for queries (e.g. `gpt-5.5-mini`, `claude-sonnet-4-6`).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *AIKnowledgeBaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*ApiClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data", "Expected *ApiClient")
		return
	}
	r.client = client
}

func (r *AIKnowledgeBaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AIKnowledgeBaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]interface{}{
		"name": plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		body["description"] = plan.Description.ValueString()
	}
	if !plan.EmbeddingModel.IsNull() && !plan.EmbeddingModel.IsUnknown() {
		body["embeddingModel"] = plan.EmbeddingModel.ValueString()
	}
	if !plan.ChatModel.IsNull() && !plan.ChatModel.IsUnknown() {
		body["chatModel"] = plan.ChatModel.ValueString()
	}

	respBody, status, err := r.client.Do("POST", "/api/ai/knowledge-bases", body)
	if err != nil || status >= 300 {
		resp.Diagnostics.AddError("KB create failed",
			fmt.Sprintf("status=%d err=%v body=%s", status, err, string(respBody)))
		return
	}

	var wrapper struct {
		Success bool                          `json:"success"`
		Data    AIKnowledgeBaseAPIRecord      `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		resp.Diagnostics.AddError("Decode KB response", err.Error())
		return
	}

	plan.ID = types.StringValue(wrapper.Data.ID)
	plan.Name = types.StringValue(wrapper.Data.Name)
	plan.EmbeddingModel = types.StringValue(wrapper.Data.EmbeddingModel)
	plan.ChatModel = types.StringValue(wrapper.Data.ChatModel)
	if wrapper.Data.Description != nil {
		plan.Description = types.StringValue(*wrapper.Data.Description)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *AIKnowledgeBaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AIKnowledgeBaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("GET", fmt.Sprintf("/api/ai/knowledge-bases/%s", state.ID.ValueString()), nil)
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil || status >= 300 {
		resp.Diagnostics.AddError("KB read failed",
			fmt.Sprintf("status=%d err=%v body=%s", status, err, string(respBody)))
		return
	}

	var wrapper struct {
		Success bool                          `json:"success"`
		Data    AIKnowledgeBaseAPIRecord      `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		resp.Diagnostics.AddError("Decode KB response", err.Error())
		return
	}

	state.Name = types.StringValue(wrapper.Data.Name)
	state.EmbeddingModel = types.StringValue(wrapper.Data.EmbeddingModel)
	state.ChatModel = types.StringValue(wrapper.Data.ChatModel)
	if wrapper.Data.Description != nil {
		state.Description = types.StringValue(*wrapper.Data.Description)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *AIKnowledgeBaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// API doesn't support PATCH yet — Update is a no-op that just propagates plan to state.
	var plan AIKnowledgeBaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *AIKnowledgeBaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AIKnowledgeBaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	respBody, status, err := r.client.Do("DELETE", fmt.Sprintf("/api/ai/knowledge-bases/%s", state.ID.ValueString()), nil)
	if status == 404 {
		return
	}
	if err != nil || status >= 300 {
		resp.Diagnostics.AddError("KB delete failed",
			fmt.Sprintf("status=%d err=%v body=%s", status, err, string(respBody)))
	}
}

type AIKnowledgeBaseAPIRecord struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	EmbeddingModel string  `json:"embeddingModel"`
	ChatModel      string  `json:"chatModel"`
}
