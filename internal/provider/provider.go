// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

var (
	_ provider.Provider                       = &MarmotProvider{}
	_ provider.ProviderWithFunctions          = &MarmotProvider{}
	_ provider.ProviderWithEphemeralResources = &MarmotProvider{}
	_ provider.ProviderWithActions            = &MarmotProvider{}
)

type MarmotProvider struct {
	version string
}

type MarmotProviderModel struct {
	Host   types.String `tfsdk:"host"`
	APIKey types.String `tfsdk:"api_key"`
	Token  types.String `tfsdk:"token"`
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
		MarkdownDescription: "Manage your [Marmot](https://marmotdata.io) catalog as code. Marmot is " +
			"the open-source context layer for agents and humans: it catalogs every service, API, " +
			"queue, topic, database, and pipeline in your organization, storing only metadata such " +
			"as schemas, ownership, descriptions, and lineage. \n\nThis provider populates Marmot from " +
			"Terraform, letting you declare assets, the lineage between them, and glossary terms " +
			"alongside the infrastructure they describe.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				MarkdownDescription: "Marmot API host URL. May also be set via the `MARMOT_HOST` " +
					"environment variable.",
				Optional: true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "The provider authenticates with a Marmot API key, set through " +
					"the `api_key` attribute or the `MARMOT_API_KEY` environment variable.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("token")),
				},
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "Marmot bearer token. May also be set via the `MARMOT_TOKEN` " +
					"environment variable. Conflicts with `api_key`.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("api_key")),
				},
			},
		},
	}
}

func (p *MarmotProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config MarmotProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sdkClient, err := marmot.NewClient(marmot.ClientOptions{
		Host:   config.Host.ValueString(),
		APIKey: config.APIKey.ValueString(),
		Token:  config.Token.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Configure Marmot Client",
			"Could not resolve Marmot credentials or host: "+err.Error()+"\n\n"+
				"Set the api_key or token attribute, the MARMOT_API_KEY or MARMOT_TOKEN "+
				"environment variable, run `marmot login`, or run in a Kubernetes pod "+
				"with a service-account token.",
		)
		return
	}

	resp.ResourceData = sdkClient
	resp.DataSourceData = sdkClient

	tflog.Info(ctx, "Configured Marmot client", map[string]any{
		"host":        sdkClient.Host(),
		"auth_source": sdkClient.Credential().Source(),
	})
}

func (p *MarmotProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAssetResource,
		NewLineageResource,
		NewGlossaryResource,
	}
}

func (p *MarmotProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *MarmotProvider) Functions(ctx context.Context) []func() function.Function {
	return nil
}

func (p *MarmotProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return nil
}

func (p *MarmotProvider) Actions(ctx context.Context) []func() action.Action {
	return nil
}
