// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &DataProductAssetResource{}
var _ resource.ResourceWithImportState = &DataProductAssetResource{}

func NewDataProductAssetResource() resource.Resource {
	return &DataProductAssetResource{}
}

// DataProductAssetResource defines the resource implementation.
type DataProductAssetResource struct {
	client *marmot.Client
}

// DataProductAssetResourceModel describes the data product asset resource data model.
type DataProductAssetResourceModel struct {
	DataProductID types.String `tfsdk:"data_product_id"`
	AssetID       types.String `tfsdk:"asset_id"`
}

func (r *DataProductAssetResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_product_asset"
}

func (r *DataProductAssetResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds a single asset to a data product by hand. For rule-based membership, " +
			"use `marmot_data_product_rule`.",

		Attributes: map[string]schema.Attribute{
			"data_product_id": schema.StringAttribute{
				MarkdownDescription: "ID of the data product",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"asset_id": schema.StringAttribute{
				MarkdownDescription: "ID of the asset to add to the data product",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *DataProductAssetResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DataProductAssetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data DataProductAssetResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DataProducts.AddAssets(ctx, data.DataProductID.ValueString(), []string{data.AssetID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to add asset to data product: %s", err))
		return
	}

	tflog.Info(ctx, "Data product asset added", map[string]any{
		"data_product_id": data.DataProductID.ValueString(),
		"asset_id":        data.AssetID.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductAssetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data DataProductAssetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.assetIsMember(ctx, data.DataProductID.ValueString(), data.AssetID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read data product assets: %s", err))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductAssetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update Not Supported",
		"Data product asset resources cannot be updated. Changes to data_product_id or asset_id require replacement.",
	)
}

func (r *DataProductAssetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data DataProductAssetResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DataProducts.RemoveAsset(ctx, data.DataProductID.ValueString(), data.AssetID.ValueString())
	if err != nil && !marmot.IsNotFound(err) {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to remove asset from data product: %s", err))
		return
	}

	tflog.Info(ctx, "Data product asset removed", map[string]any{
		"data_product_id": data.DataProductID.ValueString(),
		"asset_id":        data.AssetID.ValueString(),
	})
}

// ImportState imports a membership with the composite ID "<data_product_id>/<asset_id>".
func (r *DataProductAssetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	productID, assetID, ok := strings.Cut(req.ID, "/")
	if !ok || productID == "" || assetID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in the format 'data_product_id/asset_id', got: %s", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("data_product_id"), productID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("asset_id"), assetID)...)
}

// assetIsMember pages through the manually added assets of a data product and
// reports whether assetID is among them.
func (r *DataProductAssetResource) assetIsMember(ctx context.Context, productID, assetID string) (bool, error) {
	const pageSize = 100
	var offset int64
	for {
		page, err := r.client.DataProducts.Assets(ctx, productID, marmot.DataProductAssetsOptions{
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return false, err
		}
		if slices.Contains(page.AssetIds, assetID) {
			return true, nil
		}
		offset += int64(len(page.AssetIds))
		if len(page.AssetIds) == 0 || offset >= page.Total {
			return false, nil
		}
	}
}
