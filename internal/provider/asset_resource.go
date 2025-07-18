// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/marmotdata/terraform-provider-marmot/internal/client/client"
	"github.com/marmotdata/terraform-provider-marmot/internal/client/client/assets"
	"github.com/marmotdata/terraform-provider-marmot/internal/client/models"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &AssetResource{}
var _ resource.ResourceWithImportState = &AssetResource{}

func NewAssetResource() resource.Resource {
	return &AssetResource{}
}

// AssetResource defines the resource implementation.
type AssetResource struct {
	client *client.Marmot
}

// ExternalLink represents a link to an external resource.
type ExternalLinkModel struct {
	Icon types.String `tfsdk:"icon"`
	Name types.String `tfsdk:"name"`
	URL  types.String `tfsdk:"url"`
}

// AssetSource represents a source for an asset.
type AssetSourceModel struct {
	Name       types.String `tfsdk:"name"`
	Priority   types.Int64  `tfsdk:"priority"`
	Properties types.Map    `tfsdk:"properties"`
}

// AssetEnvironment represents an environment for an asset.
type AssetEnvironmentModel struct {
	Name     types.String `tfsdk:"name"`
	Path     types.String `tfsdk:"path"`
	Metadata types.Map    `tfsdk:"metadata"`
}

// AssetResourceModel describes the asset resource data model.
type AssetResourceModel struct {
	Name          types.String                     `tfsdk:"name"`
	Type          types.String                     `tfsdk:"type"`
	Description   types.String                     `tfsdk:"description"`
	Services      types.Set                        `tfsdk:"services"`
	Tags          types.Set                        `tfsdk:"tags"`
	Metadata      types.Map                        `tfsdk:"metadata"`
	Schema        types.Map                        `tfsdk:"schema"`
	ExternalLinks []ExternalLinkModel              `tfsdk:"external_links"`
	Sources       []AssetSourceModel               `tfsdk:"sources"`
	Environments  map[string]AssetEnvironmentModel `tfsdk:"environments"`

	ResourceID types.String `tfsdk:"resource_id"`
	CreatedAt  types.String `tfsdk:"created_at"`
	CreatedBy  types.String `tfsdk:"created_by"`
	UpdatedAt  types.String `tfsdk:"updated_at"`
	LastSyncAt types.String `tfsdk:"last_sync_at"`
	MRN        types.String `tfsdk:"mrn"`
}

func (r *AssetResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_asset"
}

func (r *AssetResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Asset resource",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Asset name",
				Required:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Asset type",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Asset description",
				Optional:            true,
			},
			"services": schema.SetAttribute{
				MarkdownDescription: "Services associated with the asset",
				Required:            true,
				ElementType:         types.StringType,
			},
			"tags": schema.SetAttribute{
				MarkdownDescription: "Tags associated with the asset",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"metadata": schema.MapAttribute{
				MarkdownDescription: "Metadata associated with the asset",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"schema": schema.MapAttribute{
				MarkdownDescription: "Schema associated with the asset",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"external_links": schema.ListNestedAttribute{
				MarkdownDescription: "External links associated with the asset",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"icon": schema.StringAttribute{
							MarkdownDescription: "Icon for the external link",
							Optional:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the external link",
							Required:            true,
						},
						"url": schema.StringAttribute{
							MarkdownDescription: "URL of the external link",
							Required:            true,
						},
					},
				},
			},
			"sources": schema.ListNestedAttribute{
				MarkdownDescription: "Sources associated with the asset",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the source",
							Required:            true,
						},
						"priority": schema.Int64Attribute{
							MarkdownDescription: "Priority of the source",
							Optional:            true,
						},
						"properties": schema.MapAttribute{
							MarkdownDescription: "Properties of the source",
							Optional:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"environments": schema.MapNestedAttribute{
				MarkdownDescription: "Environments associated with the asset",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the environment",
							Required:            true,
						},
						"path": schema.StringAttribute{
							MarkdownDescription: "Path of the environment",
							Required:            true,
						},
						"metadata": schema.MapAttribute{
							MarkdownDescription: "Metadata of the environment",
							Optional:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"resource_id": schema.StringAttribute{
				MarkdownDescription: "Resource ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_by": schema.StringAttribute{
				MarkdownDescription: "Creator",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Last update timestamp",
				Computed:            true,
			},
			"last_sync_at": schema.StringAttribute{
				MarkdownDescription: "Last sync timestamp",
				Computed:            true,
			},
			"mrn": schema.StringAttribute{
				MarkdownDescription: "Marmot Resource Name",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *AssetResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.Marmot)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Marmot, got: %T", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *AssetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AssetResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	asset, diags := r.toCreateRequest(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := assets.NewPostAssetsParams().WithAsset(asset)
	result, err := r.client.Assets.PostAssets(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create asset: %s", err))
		return
	}

	diags = r.updateModelFromResponse(ctx, &data, result.Payload)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AssetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AssetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := assets.NewGetAssetsIDParams().WithID(data.ResourceID.ValueString())
	result, err := r.client.Assets.GetAssetsID(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read asset: %s", err))
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, result.Payload)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AssetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AssetResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state AssetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	asset, diags := r.toUpdateRequest(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := assets.NewPutAssetsIDParams().
		WithID(state.ResourceID.ValueString()).
		WithAsset(asset)

	result, err := r.client.Assets.PutAssetsID(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update asset: %s", err))
		return
	}

	diags = r.updateModelFromResponse(ctx, &data, result.Payload)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AssetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AssetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := assets.NewDeleteAssetsIDParams().WithID(data.ResourceID.ValueString())
	_, err := r.client.Assets.DeleteAssetsID(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete asset: %s", err))
		return
	}

	tflog.Info(ctx, "Asset deleted", map[string]interface{}{
		"id": data.ResourceID.ValueString(),
	})
}

func (r *AssetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("resource_id"), req, resp)
}

func (r *AssetResource) toCreateRequest(ctx context.Context, data AssetResourceModel) (*models.AssetsCreateRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	var services []string
	diags.Append(data.Services.ElementsAs(ctx, &services, false)...)
	sort.Strings(services)

	var tags []string
	if !data.Tags.IsNull() {
		diags.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		sort.Strings(tags)
	}

	metadata, metadataDiags := r.mapToDictionary(data.Metadata)
	diags.Append(metadataDiags...)

	schema, schemaDiags := r.mapToDictionary(data.Schema)
	diags.Append(schemaDiags...)

	externalLinks := r.convertExternalLinks(data.ExternalLinks)

	sources := r.convertSources(data.Sources, &diags)

	environments := r.convertEnvironments(data.Environments, &diags)

	name := data.Name.ValueString()
	assetType := data.Type.ValueString()
	description := data.Description.ValueString()

	return &models.AssetsCreateRequest{
		Name:          &name,
		Type:          &assetType,
		Description:   description,
		Providers:     services,
		Tags:          tags,
		Metadata:      metadata,
		Schema:        schema,
		ExternalLinks: externalLinks,
		Sources:       sources,
		Environments:  environments,
	}, diags
}

func (r *AssetResource) toUpdateRequest(ctx context.Context, data AssetResourceModel) (*models.AssetsUpdateRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	var services []string
	diags.Append(data.Services.ElementsAs(ctx, &services, false)...)
	sort.Strings(services)

	var tags []string
	if !data.Tags.IsNull() {
		diags.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		sort.Strings(tags)
	}

	metadata, metadataDiags := r.mapToDictionary(data.Metadata)
	diags.Append(metadataDiags...)

	schema, schemaDiags := r.mapToDictionary(data.Schema)
	diags.Append(schemaDiags...)

	externalLinks := r.convertExternalLinks(data.ExternalLinks)

	sources := r.convertSources(data.Sources, &diags)

	environments := r.convertEnvironments(data.Environments, &diags)

	return &models.AssetsUpdateRequest{
		Name:          data.Name.ValueString(),
		Type:          data.Type.ValueString(),
		Description:   data.Description.ValueString(),
		Providers:     services,
		Tags:          tags,
		Metadata:      metadata,
		Schema:        schema,
		ExternalLinks: externalLinks,
		Sources:       sources,
		Environments:  environments,
	}, diags
}

func (r *AssetResource) convertExternalLinks(links []ExternalLinkModel) []*models.AssetExternalLink {
	if len(links) == 0 {
		return nil
	}

	result := make([]*models.AssetExternalLink, len(links))
	for i, link := range links {
		icon := ""
		if !link.Icon.IsNull() {
			icon = link.Icon.ValueString()
		}

		result[i] = &models.AssetExternalLink{
			Icon: icon,
			Name: link.Name.ValueString(),
			URL:  link.URL.ValueString(),
		}
	}
	return result
}

func (r *AssetResource) convertSources(sources []AssetSourceModel, diags *diag.Diagnostics) []*models.AssetAssetSource {
	if len(sources) == 0 {
		return nil
	}

	result := make([]*models.AssetAssetSource, len(sources))
	for i, source := range sources {
		props, propDiags := r.mapToDictionary(source.Properties)
		diags.Append(propDiags...)

		priority := int64(0)
		if !source.Priority.IsNull() {
			priority = source.Priority.ValueInt64()
		}

		result[i] = &models.AssetAssetSource{
			Name:       source.Name.ValueString(),
			Priority:   priority,
			Properties: props,
		}
	}
	return result
}

func (r *AssetResource) convertEnvironments(environments map[string]AssetEnvironmentModel, diags *diag.Diagnostics) map[string]models.AssetEnvironment {
	if len(environments) == 0 {
		return nil
	}

	result := make(map[string]models.AssetEnvironment)
	for k, env := range environments {
		metadata, mdDiags := r.mapToDictionary(env.Metadata)
		diags.Append(mdDiags...)

		result[k] = models.AssetEnvironment{
			Name:     env.Name.ValueString(),
			Path:     env.Path.ValueString(),
			Metadata: metadata,
		}
	}
	return result
}

func (r *AssetResource) mapToDictionary(tfMap types.Map) (map[string]interface{}, diag.Diagnostics) {
	if tfMap.IsNull() || tfMap.IsUnknown() {
		return make(map[string]interface{}), nil
	}

	elements := tfMap.Elements()
	result := make(map[string]interface{}, len(elements))

	for k, v := range elements {
		strVal, ok := v.(basetypes.StringValue)
		if !ok {
			return nil, diag.Diagnostics{
				diag.NewErrorDiagnostic(
					"Type Conversion Error",
					fmt.Sprintf("Expected string value, got %T", v),
				),
			}
		}
		result[k] = strVal.ValueString()
	}

	return result, nil
}

func (r *AssetResource) updateModelFromResponse(ctx context.Context, model *AssetResourceModel, asset *models.AssetAsset) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ResourceID = types.StringValue(asset.ID)
	model.Name = types.StringValue(asset.Name)
	model.Type = types.StringValue(asset.Type)
	model.Description = types.StringValue(asset.Description)
	model.CreatedAt = types.StringValue(asset.CreatedAt)
	model.CreatedBy = types.StringValue(asset.CreatedBy)
	model.UpdatedAt = types.StringValue(asset.UpdatedAt)
	model.MRN = types.StringValue(asset.Mrn)

	if asset.LastSyncAt != "" {
		model.LastSyncAt = types.StringValue(asset.LastSyncAt)
	} else {
		model.LastSyncAt = types.StringNull()
	}

	if asset.Providers != nil {
		sortedServices := make([]string, len(asset.Providers))
		copy(sortedServices, asset.Providers)
		sort.Strings(sortedServices)

		services, diag := types.SetValueFrom(ctx, types.StringType, sortedServices)
		diags.Append(diag...)
		model.Services = services
	} else {
		model.Services = types.SetNull(types.StringType)
	}

	if asset.Tags != nil {
		sortedTags := make([]string, len(asset.Tags))
		copy(sortedTags, asset.Tags)
		sort.Strings(sortedTags)

		tags, diag := types.SetValueFrom(ctx, types.StringType, sortedTags)
		diags.Append(diag...)
		model.Tags = tags
	} else {
		model.Tags = types.SetNull(types.StringType)
	}

	if metaMap, ok := asset.Metadata.(map[string]interface{}); ok && metaMap != nil {
		metadata, diag := types.MapValueFrom(ctx, types.StringType, r.convertMapToStringMap(metaMap))
		diags.Append(diag...)
		model.Metadata = metadata
	} else {
		model.Metadata = types.MapNull(types.StringType)
	}

	if schemaMap, ok := asset.Schema.(map[string]interface{}); ok && schemaMap != nil {
		schema, diag := types.MapValueFrom(ctx, types.StringType, r.convertMapToStringMap(schemaMap))
		diags.Append(diag...)
		model.Schema = schema
	} else {
		model.Schema = types.MapNull(types.StringType)
	}

	if len(asset.ExternalLinks) > 0 {
		model.ExternalLinks = r.convertModelExternalLinks(asset.ExternalLinks)
	} else {
		if len(model.ExternalLinks) == 0 {
			model.ExternalLinks = nil
		}
	}

	if len(asset.Sources) > 0 {
		model.Sources = r.convertModelSources(ctx, asset.Sources, &diags)
	} else {
		if len(model.Sources) == 0 {
			model.Sources = nil
		}
	}

	if len(asset.Environments) > 0 {
		model.Environments = r.convertModelEnvironments(ctx, asset.Environments, &diags)
	} else {
		model.Environments = make(map[string]AssetEnvironmentModel)
	}

	return diags
}

func (r *AssetResource) convertMapToStringMap(m map[string]interface{}) map[string]string {
	if m == nil {
		return make(map[string]string)
	}

	result := make(map[string]string)
	for k, v := range m {
		if v != nil {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}

func (r *AssetResource) convertModelExternalLinks(links []*models.AssetExternalLink) []ExternalLinkModel {
	if len(links) == 0 {
		return []ExternalLinkModel{}
	}

	result := make([]ExternalLinkModel, len(links))
	for i, link := range links {
		result[i] = ExternalLinkModel{
			Name: types.StringValue(link.Name),
			URL:  types.StringValue(link.URL),
		}

		if link.Icon != "" {
			result[i].Icon = types.StringValue(link.Icon)
		} else {
			result[i].Icon = types.StringNull()
		}
	}
	return result
}

func (r *AssetResource) convertModelSources(ctx context.Context, sources []*models.AssetAssetSource, diags *diag.Diagnostics) []AssetSourceModel {
	if len(sources) == 0 {
		return []AssetSourceModel{}
	}

	result := make([]AssetSourceModel, len(sources))
	for i, source := range sources {
		var properties types.Map

		if props, ok := source.Properties.(map[string]interface{}); ok && props != nil {
			propsMap, diag := types.MapValueFrom(ctx, types.StringType, r.convertMapToStringMap(props))
			diags.Append(diag...)
			properties = propsMap
		} else {
			properties = types.MapNull(types.StringType)
		}

		result[i] = AssetSourceModel{
			Name:       types.StringValue(source.Name),
			Priority:   types.Int64Value(source.Priority),
			Properties: properties,
		}
	}
	return result
}

func (r *AssetResource) convertModelEnvironments(ctx context.Context, environments map[string]models.AssetEnvironment, diags *diag.Diagnostics) map[string]AssetEnvironmentModel {
	if len(environments) == 0 {
		return make(map[string]AssetEnvironmentModel)
	}

	result := make(map[string]AssetEnvironmentModel)
	for k, env := range environments {
		var metadata types.Map

		if meta, ok := env.Metadata.(map[string]interface{}); ok && meta != nil {
			metaMap, diag := types.MapValueFrom(ctx, types.StringType, r.convertMapToStringMap(meta))
			diags.Append(diag...)
			metadata = metaMap
		} else {
			metadata = types.MapNull(types.StringType)
		}

		result[k] = AssetEnvironmentModel{
			Name:     types.StringValue(env.Name),
			Path:     types.StringValue(env.Path),
			Metadata: metadata,
		}
	}
	return result
}