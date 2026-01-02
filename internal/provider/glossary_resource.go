// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

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
	"github.com/marmotdata/terraform-provider-marmot/internal/client/client"
	"github.com/marmotdata/terraform-provider-marmot/internal/client/client/glossary"
	"github.com/marmotdata/terraform-provider-marmot/internal/client/models"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GlossaryResource{}
var _ resource.ResourceWithImportState = &GlossaryResource{}

func NewGlossaryResource() resource.Resource {
	return &GlossaryResource{}
}

// GlossaryResource defines the resource implementation.
type GlossaryResource struct {
	client *client.Marmot
}

// OwnerModel represents an owner of a glossary term.
type OwnerModel struct {
	ID   types.String `tfsdk:"id"`
	Type types.String `tfsdk:"type"`
}

// GlossaryResourceModel describes the glossary resource data model.
type GlossaryResourceModel struct {
	Name         types.String `tfsdk:"name"`
	Definition   types.String `tfsdk:"definition"`
	Description  types.String `tfsdk:"description"`
	ParentTermID types.String `tfsdk:"parent_term_id"`
	Owners       []OwnerModel `tfsdk:"owners"`
	Metadata     types.Map    `tfsdk:"metadata"`
	ID           types.String `tfsdk:"id"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func (r *GlossaryResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glossary_term"
}

func (r *GlossaryResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Glossary term resource for defining business terminology",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the glossary term",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"definition": schema.StringAttribute{
				MarkdownDescription: "Definition of the glossary term",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Additional description for the glossary term",
				Optional:            true,
			},
			"parent_term_id": schema.StringAttribute{
				MarkdownDescription: "ID of the parent glossary term for hierarchical organization",
				Optional:            true,
			},
			"owners": schema.ListNestedAttribute{
				MarkdownDescription: "Owners of the glossary term",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "ID of the owner (user or team ID)",
							Required:            true,
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Type of owner: 'user' or 'team'",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("user", "team"),
							},
						},
					},
				},
			},
			"metadata": schema.MapAttribute{
				MarkdownDescription: "Metadata associated with the glossary term",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Glossary term ID",
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

func (r *GlossaryResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GlossaryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GlossaryResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	term, diags := r.toCreateRequest(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := glossary.NewPostGlossaryParams().WithTerm(term)
	result, err := r.client.Glossary.PostGlossary(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create glossary term: %s", err))
		return
	}

	if result.Payload.ID == "" {
		resp.Diagnostics.AddError("API Error", "Glossary term created but no ID returned")
		return
	}

	diags = r.updateModelFromResponse(ctx, &data, result.Payload)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Glossary term created", map[string]interface{}{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GlossaryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GlossaryResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.ValueString() == "" {
		resp.Diagnostics.AddError("Configuration Error", "id is required for read operation")
		return
	}

	params := glossary.NewGetGlossaryIDParams().WithID(data.ID.ValueString())
	result, err := r.client.Glossary.GetGlossaryID(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read glossary term: %s", err))
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, result.Payload)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GlossaryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GlossaryResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state GlossaryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	term, diags := r.toUpdateRequest(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := glossary.NewPutGlossaryIDParams().
		WithID(state.ID.ValueString()).
		WithTerm(term)

	result, err := r.client.Glossary.PutGlossaryID(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update glossary term: %s", err))
		return
	}

	diags = r.updateModelFromResponse(ctx, &data, result.Payload)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Glossary term updated", map[string]interface{}{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GlossaryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GlossaryResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := glossary.NewDeleteGlossaryIDParams().WithID(data.ID.ValueString())
	_, err := r.client.Glossary.DeleteGlossaryID(params)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete glossary term: %s", err))
		return
	}

	tflog.Info(ctx, "Glossary term deleted", map[string]interface{}{
		"id": data.ID.ValueString(),
	})
}

func (r *GlossaryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *GlossaryResource) toCreateRequest(ctx context.Context, data GlossaryResourceModel) (*models.V1GlossaryCreateTermRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	name := data.Name.ValueString()
	definition := data.Definition.ValueString()

	req := &models.V1GlossaryCreateTermRequest{
		Name:       &name,
		Definition: &definition,
	}

	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		req.Description = data.Description.ValueString()
	}

	if !data.ParentTermID.IsNull() && !data.ParentTermID.IsUnknown() {
		req.ParentTermID = data.ParentTermID.ValueString()
	}

	if len(data.Owners) > 0 {
		owners := make([]*models.V1GlossaryOwnerRequest, len(data.Owners))
		for i, owner := range data.Owners {
			id := owner.ID.ValueString()
			ownerType := owner.Type.ValueString()
			owners[i] = &models.V1GlossaryOwnerRequest{
				ID:   &id,
				Type: &ownerType,
			}
		}
		req.Owners = owners
	}

	if !data.Metadata.IsNull() && !data.Metadata.IsUnknown() {
		metadata := make(map[string]interface{})
		for k, v := range data.Metadata.Elements() {
			if strVal, ok := v.(types.String); ok && !strVal.IsNull() {
				metadata[k] = strVal.ValueString()
			}
		}
		if len(metadata) > 0 {
			req.Metadata = metadata
		}
	}

	return req, diags
}

func (r *GlossaryResource) toUpdateRequest(ctx context.Context, data GlossaryResourceModel) (*models.V1GlossaryUpdateTermRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	req := &models.V1GlossaryUpdateTermRequest{
		Name:       data.Name.ValueString(),
		Definition: data.Definition.ValueString(),
	}

	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		req.Description = data.Description.ValueString()
	}

	if !data.ParentTermID.IsNull() && !data.ParentTermID.IsUnknown() {
		req.ParentTermID = data.ParentTermID.ValueString()
	}

	if len(data.Owners) > 0 {
		owners := make([]*models.V1GlossaryOwnerRequest, len(data.Owners))
		for i, owner := range data.Owners {
			id := owner.ID.ValueString()
			ownerType := owner.Type.ValueString()
			owners[i] = &models.V1GlossaryOwnerRequest{
				ID:   &id,
				Type: &ownerType,
			}
		}
		req.Owners = owners
	}

	if !data.Metadata.IsNull() && !data.Metadata.IsUnknown() {
		metadata := make(map[string]interface{})
		for k, v := range data.Metadata.Elements() {
			if strVal, ok := v.(types.String); ok && !strVal.IsNull() {
				metadata[k] = strVal.ValueString()
			}
		}
		if len(metadata) > 0 {
			req.Metadata = metadata
		}
	}

	return req, diags
}

func (r *GlossaryResource) updateModelFromResponse(ctx context.Context, model *GlossaryResourceModel, term *models.GlossaryGlossaryTerm) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(term.ID)
	model.Name = types.StringValue(term.Name)
	model.Definition = types.StringValue(term.Definition)
	model.CreatedAt = types.StringValue(term.CreatedAt)
	model.UpdatedAt = types.StringValue(term.UpdatedAt)

	if term.Description != "" {
		model.Description = types.StringValue(term.Description)
	} else {
		model.Description = types.StringNull()
	}

	if term.ParentTermID != "" {
		model.ParentTermID = types.StringValue(term.ParentTermID)
	} else {
		model.ParentTermID = types.StringNull()
	}

	if len(term.Owners) > 0 {
		owners := make([]OwnerModel, len(term.Owners))
		for i, owner := range term.Owners {
			owners[i] = OwnerModel{
				ID:   types.StringValue(owner.ID),
				Type: types.StringValue(owner.Type),
			}
		}
		model.Owners = owners
	} else {
		model.Owners = nil
	}

	if metaMap, ok := term.Metadata.(map[string]interface{}); ok && metaMap != nil && len(metaMap) > 0 {
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
