package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &DevskinProvider{}

type DevskinProvider struct {
	version string
}

type DevskinProviderModel struct {
	ApiURL types.String `tfsdk:"api_url"`
	Token  types.String `tfsdk:"token"`
}

// ApiClient holds the HTTP client and configuration for API calls.
type ApiClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &DevskinProvider{
			version: version,
		}
	}
}

func (p *DevskinProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "devskin"
	resp.Version = p.version
}

func (p *DevskinProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for managing DevskinCloud resources.",
		Attributes: map[string]schema.Attribute{
			"api_url": schema.StringAttribute{
				Description: "The base URL of the DevskinCloud API. Defaults to https://cloud-api.devskin.com/api",
				Optional:    true,
			},
			"token": schema.StringAttribute{
				Description: "The Bearer token for authenticating with the DevskinCloud API.",
				Required:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *DevskinProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config DevskinProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiURL := "https://cloud-api.devskin.com/api"
	if !config.ApiURL.IsNull() && !config.ApiURL.IsUnknown() {
		apiURL = config.ApiURL.ValueString()
	}

	apiURL = strings.TrimRight(apiURL, "/")

	if config.Token.IsNull() || config.Token.IsUnknown() {
		resp.Diagnostics.AddError(
			"Missing API Token",
			"The provider requires a valid API token to authenticate with the DevskinCloud API.",
		)
		return
	}

	client := &ApiClient{
		BaseURL:    apiURL,
		Token:      config.Token.ValueString(),
		HTTPClient: &http.Client{},
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *DevskinProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewInstanceResource,
		NewDatabaseResource,
		NewK8sClusterResource,
		NewContainerServiceResource,
		NewContainerClusterResource,
		NewTaskDefinitionResource,
		NewVPCResource,
		NewFlexServiceResource,
		NewElasticIPResource,
		NewIAMRoleResource,
	}
}

func (p *DevskinProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewInstancesDataSource,
	}
}

// --- HTTP helper methods on ApiClient ---

func (c *ApiClient) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	url := fmt.Sprintf("%s%s", c.BaseURL, path)

	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

func (c *ApiClient) Get(path string) ([]byte, int, error) {
	return c.doRequest(http.MethodGet, path, nil)
}

func (c *ApiClient) Post(path string, body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPost, path, body)
}

func (c *ApiClient) Put(path string, body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPut, path, body)
}

func (c *ApiClient) Patch(path string, body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPatch, path, body)
}

func (c *ApiClient) Delete(path string) ([]byte, int, error) {
	return c.doRequest(http.MethodDelete, path, nil)
}
