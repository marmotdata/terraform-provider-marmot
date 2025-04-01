// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/marmotdata/terraform-provider-marmot/internal/client/client"
)

var _ provider.Provider = &MarmotProvider{}

type MarmotProvider struct {
	version string
}

type MarmotProviderModel struct {
	Host   types.String `tfsdk:"host"`
	APIKey types.String `tfsdk:"api_key"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MarmotProvider{
			version: version,
		}
	}
}

func (p *MarmotProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "marmot"
	resp.Version = p.version
}

func (p *MarmotProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				MarkdownDescription: "Marmot API host URL",
				Required:            true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Marmot API key for authentication",
				Required:            true,
				Sensitive:           true,
			},
		},
	}
}

func (p *MarmotProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config MarmotProviderModel

	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	if config.Host.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Marmot API Host",
			"The provider cannot create the Marmot API client without a host",
		)
	}

	if config.APIKey.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Marmot API Key",
			"The provider cannot create the Marmot API client without an API key",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	host := config.Host.ValueString()
	apiKey := config.APIKey.ValueString()

	scheme := "https"
	if strings.HasPrefix(host, "http://") {
		scheme = "http"
		host = strings.TrimPrefix(host, "http://")
	} else if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	}

	transport := httptransport.New(host, "/api/v1", []string{scheme})
	transport.DefaultAuthentication = httptransport.APIKeyAuth("X-API-Key", "header", apiKey)
	marmotClient := client.New(transport, nil)

	resp.ResourceData = marmotClient
	resp.DataSourceData = marmotClient

	tflog.Info(ctx, "Configured Marmot client", map[string]interface{}{
		"host": host,
	})
}

func (p *MarmotProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAssetResource,
		NewLineageResource,
	}
}

func (p *MarmotProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

