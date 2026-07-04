// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &DataProductResource{}
var _ resource.ResourceWithImportState = &DataProductResource{}

func NewDataProductResource() resource.Resource {
	return &DataProductResource{}
}

// DataProductResource defines the resource implementation.
type DataProductResource struct {
	client *marmot.Client
}

// DataProductResourceModel describes the data product resource data model.
type DataProductResourceModel struct {
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Tags         types.Set    `tfsdk:"tags"`
	OwnerTeamIDs types.Set    `tfsdk:"owner_team_ids"`
	OwnerUserIDs types.Set    `tfsdk:"owner_user_ids"`
	Metadata     types.Map    `tfsdk:"metadata"`
	ID           types.String `tfsdk:"id"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func (r *DataProductResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_product"
}

func (r *DataProductResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Groups related assets into a data product. Add assets to it directly with " +
			"`marmot_data_product_asset`, or match them dynamically with `marmot_data_product_rule`.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the data product",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the data product",
				Optional:            true,
			},
			"tags": schema.SetAttribute{
				MarkdownDescription: "Tags associated with the data product",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"owner_team_ids": schema.SetAttribute{
				MarkdownDescription: "IDs of teams that own the data product.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
			},
			"owner_user_ids": schema.SetAttribute{
				MarkdownDescription: "IDs of users that own the data product. Defaults to the calling " +
					"user when no owners are set.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
			},
			"metadata": schema.MapAttribute{
				MarkdownDescription: "Key/value metadata for the data product. The API can't clear " +
					"metadata on update: once set, removing every key leaves the old values in place " +
					"until the product is replaced.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Data product ID",
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
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Last update timestamp",
				Computed:            true,
			},
		},
	}
}

func (r *DataProductResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *marmot.Client, got: %T", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *DataProductResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data DataProductResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	product, err := r.client.DataProducts.Create(ctx, marmot.CreateDataProductInput{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
		Tags:        dataProductTagsOrEmpty(ctx, data.Tags, &resp.Diagnostics),
		Owners:      dataProductOwners(ctx, data.OwnerTeamIDs, data.OwnerUserIDs, &resp.Diagnostics),
		Metadata:    dataProductMetadata(data.Metadata),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create data product: %s", err))
		return
	}

	if product.ID == "" {
		resp.Diagnostics.AddError("API Error", "Data product created but no ID returned")
		return
	}

	applyDataProductComputedFields(&data, product)
	setDataProductOwnerSets(ctx, &data, product, &resp.Diagnostics)

	tflog.Info(ctx, "Data product created", map[string]any{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data DataProductResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("Configuration Error", "id is required for read operation")
		return
	}

	product, err := r.client.DataProducts.Get(ctx, data.ID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read data product: %s", err))
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, product)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data DataProductResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state DataProductResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	product, err := r.client.DataProducts.Update(ctx, state.ID.ValueString(), marmot.UpdateDataProductInput{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
		Tags:        dataProductTagsOrEmpty(ctx, data.Tags, &resp.Diagnostics),
		Owners:      dataProductOwners(ctx, data.OwnerTeamIDs, data.OwnerUserIDs, &resp.Diagnostics),
		Metadata:    dataProductMetadata(data.Metadata),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update data product: %s", err))
		return
	}

	applyDataProductComputedFields(&data, product)
	setDataProductOwnerSets(ctx, &data, product, &resp.Diagnostics)

	tflog.Info(ctx, "Data product updated", map[string]any{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data DataProductResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DataProducts.Delete(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete data product: %s", err))
		return
	}

	tflog.Info(ctx, "Data product deleted", map[string]any{
		"id": data.ID.ValueString(),
	})
}

func (r *DataProductResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// dataProductTags converts the Terraform tag set into a sorted string slice,
// returning nil when unset so no tags are sent.
func dataProductTags(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var tags []string
	diags.Append(set.ElementsAs(ctx, &tags, false)...)
	sort.Strings(tags)
	return tags
}

// dataProductTagsOrEmpty is like dataProductTags but returns a non-nil empty
// slice when the set is unset. The API's tags column is NOT NULL, so create
// must always send an array (a nil would be rejected), and update must send an
// explicit empty array to clear tags a user has deleted from their config.
func dataProductTagsOrEmpty(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	tags := dataProductTags(ctx, set, diags)
	if tags == nil {
		return []string{}
	}
	return tags
}

// setDataProductOwnerSets populates the team and user owner sets from an API
// response. Owners are server-controlled (an unset owner list defaults to the
// calling user), so both sets are always taken from the response.
func setDataProductOwnerSets(ctx context.Context, model *DataProductResourceModel, product *marmot.DataProduct, diags *diag.Diagnostics) {
	var teamIDs, userIDs []string
	for _, owner := range product.Owners {
		switch owner.Type {
		case "team":
			teamIDs = append(teamIDs, owner.ID)
		case "user":
			userIDs = append(userIDs, owner.ID)
		}
	}
	model.OwnerTeamIDs = stringsToSet(ctx, teamIDs, diags)
	model.OwnerUserIDs = stringsToSet(ctx, userIDs, diags)
}

// dataProductOwners builds the SDK owner list from the team and user ID sets,
// returning nil when both are empty so the server applies its default owner.
func dataProductOwners(ctx context.Context, teamIDs, userIDs types.Set, diags *diag.Diagnostics) []marmot.ProductOwner {
	var out []marmot.ProductOwner
	for _, id := range setStrings(ctx, teamIDs, diags) {
		out = append(out, marmot.ProductOwner{ID: id, Type: "team"})
	}
	for _, id := range setStrings(ctx, userIDs, diags) {
		out = append(out, marmot.ProductOwner{ID: id, Type: "user"})
	}
	return out
}

// dataProductMetadata converts a Terraform string map into the SDK metadata map,
// returning nil when unset so no metadata is sent.
func dataProductMetadata(m types.Map) map[string]any {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	out := make(map[string]any)
	for k, v := range m.Elements() {
		if strVal, ok := v.(types.String); ok && !strVal.IsNull() {
			out[k] = strVal.ValueString()
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyDataProductComputedFields copies the server-generated (read-only)
// attributes from an API response onto the model, leaving every configured
// attribute untouched. Create and Update use this so plan values, including
// nulls, are saved to state exactly as written; only unknown (computed)
// values may change after apply.
func applyDataProductComputedFields(model *DataProductResourceModel, product *marmot.DataProduct) {
	model.ID = types.StringValue(product.ID)
	model.CreatedAt = types.StringValue(product.CreatedAt)
	model.UpdatedAt = types.StringValue(product.UpdatedAt)
}

func (r *DataProductResource) updateModelFromResponse(ctx context.Context, model *DataProductResourceModel, product *marmot.DataProduct) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(product.ID)
	model.Name = types.StringValue(product.Name)
	model.CreatedAt = types.StringValue(product.CreatedAt)
	model.UpdatedAt = types.StringValue(product.UpdatedAt)

	if product.Description != "" {
		model.Description = types.StringValue(product.Description)
	} else {
		model.Description = types.StringNull()
	}

	if len(product.Tags) > 0 {
		sortedTags := make([]string, len(product.Tags))
		copy(sortedTags, product.Tags)
		sort.Strings(sortedTags)

		tags, diag := types.SetValueFrom(ctx, types.StringType, sortedTags)
		diags.Append(diag...)
		model.Tags = tags
	} else {
		model.Tags = types.SetNull(types.StringType)
	}

	setDataProductOwnerSets(ctx, model, product, &diags)

	if metaMap, ok := product.Metadata.(map[string]any); ok && len(metaMap) > 0 {
		strMap := make(map[string]string)
		for k, v := range metaMap {
			if strVal, ok := v.(string); ok {
				strMap[k] = strVal
			} else {
				strMap[k] = fmt.Sprintf("%v", v)
			}
		}
		metadata, diag := types.MapValueFrom(ctx, types.StringType, strMap)
		diags.Append(diag...)
		model.Metadata = metadata
	} else {
		model.Metadata = types.MapNull(types.StringType)
	}

	return diags
}
