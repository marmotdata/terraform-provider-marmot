// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	Name            types.String                     `tfsdk:"name"`
	Type            types.String                     `tfsdk:"type"`
	Description     types.String                     `tfsdk:"description"`
	UserDescription types.String                     `tfsdk:"user_description"`
	Services        types.Set                        `tfsdk:"services"`
	Tags            types.Set                        `tfsdk:"tags"`
	Metadata        types.Map                        `tfsdk:"metadata"`
	Schema          types.Map                        `tfsdk:"schema"`
	ExternalLinks   []ExternalLinkModel              `tfsdk:"external_links"`
	Sources         []AssetSourceModel               `tfsdk:"sources"`
	Environments    map[string]AssetEnvironmentModel `tfsdk:"environments"`

	ID            types.String `tfsdk:"id"`
	CreatedAt     types.String `tfsdk:"created_at"`
	CreatedBy     types.String `tfsdk:"created_by"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
	LastSyncAt    types.String `tfsdk:"last_sync_at"`
	MRN           types.String `tfsdk:"mrn"`
	ParentMRN     types.String `tfsdk:"parent_mrn"`
	Query         types.String `tfsdk:"query"`
	QueryLanguage types.String `tfsdk:"query_language"`
	HasRunHistory types.Bool   `tfsdk:"has_run_history"`
	IsStub        types.Bool   `tfsdk:"is_stub"`
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
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Asset type",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 100),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Asset description",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(1000),
				},
			},
			"user_description": schema.StringAttribute{
				MarkdownDescription: "User-provided description for the asset",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(2000),
				},
			},
			"services": schema.SetAttribute{
				MarkdownDescription: "Services associated with the asset",
				Required:            true,
				ElementType:         types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 100)),
				},
			},
			"tags": schema.SetAttribute{
				MarkdownDescription: "Tags associated with the asset",
				Optional:            true,
				ElementType:         types.StringType,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 100)),
				},
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
							Validators: []validator.String{
								stringvalidator.LengthBetween(1, 255),
							},
						},
						"url": schema.StringAttribute{
							MarkdownDescription: "URL of the external link",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.LengthBetween(1, 2048),
							},
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
							Validators: []validator.String{
								stringvalidator.LengthBetween(1, 255),
							},
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
							Validators: []validator.String{
								stringvalidator.LengthBetween(1, 255),
							},
						},
						"path": schema.StringAttribute{
							MarkdownDescription: "Path of the environment",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.LengthBetween(1, 500),
							},
						},
						"metadata": schema.MapAttribute{
							MarkdownDescription: "Metadata of the environment",
							Optional:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Asset ID",
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
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"last_sync_at": schema.StringAttribute{
				MarkdownDescription: "Last sync timestamp",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"mrn": schema.StringAttribute{
				MarkdownDescription: "Marmot Resource Name",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"parent_mrn": schema.StringAttribute{
				MarkdownDescription: "Parent asset's Marmot Resource Name",
				Computed:            true,
			},
			"query": schema.StringAttribute{
				MarkdownDescription: "Query associated with the asset",
				Computed:            true,
			},
			"query_language": schema.StringAttribute{
				MarkdownDescription: "Query language used for the asset's query",
				Computed:            true,
			},
			"has_run_history": schema.BoolAttribute{
				MarkdownDescription: "Whether the asset has run history",
				Computed:            true,
			},
			"is_stub": schema.BoolAttribute{
				MarkdownDescription: "Whether the asset is a stub",
				Computed:            true,
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

	if result.Payload.ID == "" {
		resp.Diagnostics.AddError("API Error", "Asset created but no ID returned")
		return
	}

	diags = r.updateModelFromResponse(ctx, &data, result.Payload)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Asset created", map[string]interface{}{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AssetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AssetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("Configuration Error", "id is required for read operation")
		return
	}

	params := assets.NewGetAssetsIDParams().WithID(data.ID.ValueString())
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
		WithID(state.ID.ValueString()).
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

	tflog.Info(ctx, "Asset updated", map[string]interface{}{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AssetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AssetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := assets.NewDeleteAssetsIDParams().WithID(data.ID.ValueString())
	_, err := r.client.Assets.DeleteAssetsID(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete asset: %s", err))
		return
	}

	tflog.Info(ctx, "Asset deleted", map[string]interface{}{
		"id": data.ID.ValueString(),
	})
}

func (r *AssetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *AssetResource) toCreateRequest(ctx context.Context, data AssetResourceModel) (*models.V1AssetsCreateRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	var services []string
	diags.Append(data.Services.ElementsAs(ctx, &services, false)...)
	sort.Strings(services)

	var tags []string
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		diags.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		sort.Strings(tags)
	}

	metadata, metadataDiags := r.mapToDictionary(data.Metadata)
	diags.Append(metadataDiags...)

	schema := r.mapToStringMap(data.Schema)

	externalLinks := r.convertExternalLinks(data.ExternalLinks)
	sources := r.convertSources(data.Sources, &diags)
	environments := r.convertEnvironments(data.Environments, &diags)

	name := data.Name.ValueString()
	assetType := data.Type.ValueString()
	description := data.Description.ValueString()

	return &models.V1AssetsCreateRequest{
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

func (r *AssetResource) toUpdateRequest(ctx context.Context, data AssetResourceModel) (*models.V1AssetsUpdateRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	var services []string
	diags.Append(data.Services.ElementsAs(ctx, &services, false)...)
	sort.Strings(services)

	var tags []string
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		diags.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		sort.Strings(tags)
	}

	metadata, metadataDiags := r.mapToDictionary(data.Metadata)
	diags.Append(metadataDiags...)

	schema := r.mapToStringMap(data.Schema)

	externalLinks := r.convertExternalLinks(data.ExternalLinks)
	sources := r.convertSources(data.Sources, &diags)
	environments := r.convertEnvironments(data.Environments, &diags)

	userDescription := ""
	if !data.UserDescription.IsNull() && !data.UserDescription.IsUnknown() {
		userDescription = data.UserDescription.ValueString()
	}

	return &models.V1AssetsUpdateRequest{
		Name:            data.Name.ValueString(),
		Type:            data.Type.ValueString(),
		Description:     data.Description.ValueString(),
		UserDescription: userDescription,
		Providers:       services,
		Tags:            tags,
		Metadata:        metadata,
		Schema:          schema,
		ExternalLinks:   externalLinks,
		Sources:         sources,
		Environments:    environments,
	}, diags
}

func (r *AssetResource) convertExternalLinks(links []ExternalLinkModel) []*models.AssetExternalLink {
	if len(links) == 0 {
		return nil
	}

	result := make([]*models.AssetExternalLink, len(links))
	for i, link := range links {
		icon := ""
		if !link.Icon.IsNull() && !link.Icon.IsUnknown() {
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
		if !source.Priority.IsNull() && !source.Priority.IsUnknown() {
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
		return nil, nil
	}

	elements := tfMap.Elements()
	if len(elements) == 0 {
		return nil, nil
	}

	// Sort keys for consistent ordering
	var keys []string
	for k := range elements {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make(map[string]interface{}, len(elements))
	for _, k := range keys {
		v := elements[k]
		strVal, ok := v.(basetypes.StringValue)
		if !ok {
			return nil, diag.Diagnostics{
				diag.NewErrorDiagnostic(
					"Type Conversion Error",
					fmt.Sprintf("Expected string value, got %T", v),
				),
			}
		}
		if !strVal.IsNull() && !strVal.IsUnknown() {
			result[k] = strVal.ValueString()
		}
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func (r *AssetResource) mapToStringMap(tfMap types.Map) map[string]string {
	if tfMap.IsNull() || tfMap.IsUnknown() {
		return nil
	}

	elements := tfMap.Elements()
	if len(elements) == 0 {
		return nil
	}

	result := make(map[string]string, len(elements))
	for k, v := range elements {
		strVal, ok := v.(basetypes.StringValue)
		if ok && !strVal.IsNull() && !strVal.IsUnknown() {
			result[k] = strVal.ValueString()
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeTimestamp(timestamp string) string {
	if timestamp == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return timestamp
	}
	return t.Format("2006-01-02T15:04:05.000000Z07:00")
}

func (r *AssetResource) updateModelFromResponse(ctx context.Context, model *AssetResourceModel, asset *models.AssetAsset) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(asset.ID)
	model.Name = types.StringValue(asset.Name)
	model.Type = types.StringValue(asset.Type)
	model.Description = types.StringValue(asset.Description)
	model.CreatedAt = types.StringValue(normalizeTimestamp(asset.CreatedAt))
	model.CreatedBy = types.StringValue(asset.CreatedBy)
	model.UpdatedAt = types.StringValue(normalizeTimestamp(asset.UpdatedAt))
	model.MRN = types.StringValue(asset.Mrn)

	// New fields
	if asset.UserDescription != "" {
		model.UserDescription = types.StringValue(asset.UserDescription)
	} else {
		model.UserDescription = types.StringNull()
	}

	if asset.ParentMrn != "" {
		model.ParentMRN = types.StringValue(asset.ParentMrn)
	} else {
		model.ParentMRN = types.StringNull()
	}

	if asset.Query != "" {
		model.Query = types.StringValue(asset.Query)
	} else {
		model.Query = types.StringNull()
	}

	if asset.QueryLanguage != "" {
		model.QueryLanguage = types.StringValue(asset.QueryLanguage)
	} else {
		model.QueryLanguage = types.StringNull()
	}

	model.HasRunHistory = types.BoolValue(asset.HasRunHistory)
	model.IsStub = types.BoolValue(asset.IsStub)

	if asset.LastSyncAt != "" {
		model.LastSyncAt = types.StringValue(normalizeTimestamp(asset.LastSyncAt))
	} else {
		model.LastSyncAt = types.StringNull()
	}

	if len(asset.Providers) > 0 {
		sortedServices := make([]string, len(asset.Providers))
		copy(sortedServices, asset.Providers)
		sort.Strings(sortedServices)

		services, diag := types.SetValueFrom(ctx, types.StringType, sortedServices)
		diags.Append(diag...)
		model.Services = services
	} else {
		services, diag := types.SetValueFrom(ctx, types.StringType, []string{})
		diags.Append(diag...)
		model.Services = services
	}

	if len(asset.Tags) > 0 {
		sortedTags := make([]string, len(asset.Tags))
		copy(sortedTags, asset.Tags)
		sort.Strings(sortedTags)

		tags, diag := types.SetValueFrom(ctx, types.StringType, sortedTags)
		diags.Append(diag...)
		model.Tags = tags
	} else {
		tags, diag := types.SetValueFrom(ctx, types.StringType, []string{})
		diags.Append(diag...)
		model.Tags = tags
	}

	if metaMap, ok := asset.Metadata.(map[string]interface{}); ok && metaMap != nil && len(metaMap) > 0 {
		sortedMeta := r.convertMapToStringMapSorted(metaMap)
		metadata, diag := types.MapValueFrom(ctx, types.StringType, sortedMeta)
		diags.Append(diag...)
		model.Metadata = metadata
	} else {
		metadata, diag := types.MapValueFrom(ctx, types.StringType, map[string]string{})
		diags.Append(diag...)
		model.Metadata = metadata
	}

	if len(asset.Schema) > 0 {
		schema, diag := types.MapValueFrom(ctx, types.StringType, asset.Schema)
		diags.Append(diag...)
		model.Schema = schema
	} else {
		schema, diag := types.MapValueFrom(ctx, types.StringType, map[string]string{})
		diags.Append(diag...)
		model.Schema = schema
	}

	if len(asset.ExternalLinks) > 0 {
		model.ExternalLinks = r.convertModelExternalLinks(asset.ExternalLinks)
	} else {
		model.ExternalLinks = nil
	}

	if len(asset.Sources) > 0 {
		model.Sources = r.convertModelSources(ctx, asset.Sources, &diags)
	} else {
		model.Sources = nil
	}

	if len(asset.Environments) > 0 {
		model.Environments = r.convertModelEnvironments(ctx, asset.Environments, &diags)
	} else {
		model.Environments = make(map[string]AssetEnvironmentModel)
	}

	return diags
}

func (r *AssetResource) convertMapToStringMapSorted(m map[string]interface{}) map[string]string {
	if m == nil {
		return make(map[string]string)
	}

	result := make(map[string]string)

	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		if v != nil {
			switch val := v.(type) {
			case string:
				if val != "" {
					result[k] = val
				}
			case int, int32, int64:
				result[k] = fmt.Sprintf("%d", val)
			case float32, float64:
				result[k] = fmt.Sprintf("%.6f", val)
			case bool:
				result[k] = fmt.Sprintf("%t", val)
			default:
				strVal := fmt.Sprintf("%v", val)
				if strVal != "" {
					result[k] = strVal
				}
			}
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

		if props, ok := source.Properties.(map[string]interface{}); ok && props != nil && len(props) > 0 {
			propsMap, diag := types.MapValueFrom(ctx, types.StringType, r.convertMapToStringMapSorted(props))
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

		if meta, ok := env.Metadata.(map[string]interface{}); ok && meta != nil && len(meta) > 0 {
			metaMap, diag := types.MapValueFrom(ctx, types.StringType, r.convertMapToStringMapSorted(meta))
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
