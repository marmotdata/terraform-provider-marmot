// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

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
var _ resource.Resource = &LineageResource{}
var _ resource.ResourceWithImportState = &LineageResource{}

func NewLineageResource() resource.Resource {
	return &LineageResource{}
}

// LineageResource defines the resource implementation.
type LineageResource struct {
	client *marmot.Client
}

// LineageResourceModel describes the lineage resource data model.
type LineageResourceModel struct {
	Source types.String `tfsdk:"source"`
	Target types.String `tfsdk:"target"`
	ID     types.String `tfsdk:"id"`
}

func (r *LineageResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lineage"
}

func (r *LineageResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lineage resource representing a connection between two assets",

		Attributes: map[string]schema.Attribute{
			"source": schema.StringAttribute{
				MarkdownDescription: "Source asset",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target": schema.StringAttribute{
				MarkdownDescription: "Target asset",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Lineage ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *LineageResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *LineageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data LineageResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	edge, err := r.client.Lineage.Write(ctx, marmot.WriteEdgeInput{
		Source: data.Source.ValueString(),
		Target: data.Target.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create lineage: %s", err))
		return
	}

	data.ID = types.StringValue(edge.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LineageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data LineageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	edge, err := r.client.Lineage.Edge(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read lineage: %s", err))
		return
	}

	data.Source = types.StringValue(edge.Source)
	data.Target = types.StringValue(edge.Target)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LineageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update Not Supported",
		"Lineage resources cannot be updated. Changes to source or target require replacement.",
	)
}

func (r *LineageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data LineageResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Lineage.Delete(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete lineage: %s", err))
		return
	}

	tflog.Info(ctx, "Lineage deleted", map[string]interface{}{
		"id": data.ID.ValueString(),
	})
}

func (r *LineageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
