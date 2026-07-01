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
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GlossaryResource{}
var _ resource.ResourceWithImportState = &GlossaryResource{}

func NewGlossaryResource() resource.Resource {
	return &GlossaryResource{}
}

// GlossaryResource defines the resource implementation.
type GlossaryResource struct {
	client *marmot.Client
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

func (r *GlossaryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GlossaryResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	term, err := r.client.Glossary.Create(ctx, r.toCreateRequest(ctx, data))
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create glossary term: %s", err))
		return
	}

	if term.ID == "" {
		resp.Diagnostics.AddError("API Error", "Glossary term created but no ID returned")
		return
	}

	applyGlossaryComputedFields(&data, term)

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

	term, err := r.client.Glossary.Get(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read glossary term: %s", err))
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, term)
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

	term, err := r.client.Glossary.Update(ctx, state.ID.ValueString(), r.toUpdateRequest(ctx, data))
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update glossary term: %s", err))
		return
	}

	applyGlossaryComputedFields(&data, term)

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

	if err := r.client.Glossary.Delete(ctx, data.ID.ValueString()); err != nil {
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

func (r *GlossaryResource) toCreateRequest(_ context.Context, data GlossaryResourceModel) marmot.CreateTermInput {
	in := marmot.CreateTermInput{
		Name:       data.Name.ValueString(),
		Definition: data.Definition.ValueString(),
		Owners:     glossaryOwners(data.Owners),
		Metadata:   glossaryMetadata(data.Metadata),
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		in.Description = data.Description.ValueString()
	}
	if !data.ParentTermID.IsNull() && !data.ParentTermID.IsUnknown() {
		in.ParentTermID = data.ParentTermID.ValueString()
	}
	return in
}

func (r *GlossaryResource) toUpdateRequest(_ context.Context, data GlossaryResourceModel) marmot.UpdateTermInput {
	in := marmot.UpdateTermInput{
		Name:       data.Name.ValueString(),
		Definition: data.Definition.ValueString(),
		Owners:     glossaryOwners(data.Owners),
		Metadata:   glossaryMetadata(data.Metadata),
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		in.Description = data.Description.ValueString()
	}
	if !data.ParentTermID.IsNull() && !data.ParentTermID.IsUnknown() {
		in.ParentTermID = data.ParentTermID.ValueString()
	}
	return in
}

// glossaryOwners converts the resource model's owners into SDK term owners.
func glossaryOwners(owners []OwnerModel) []marmot.TermOwner {
	if len(owners) == 0 {
		return nil
	}
	out := make([]marmot.TermOwner, len(owners))
	for i, o := range owners {
		out[i] = marmot.TermOwner{ID: o.ID.ValueString(), Type: o.Type.ValueString()}
	}
	return out
}

// glossaryMetadata converts a Terraform string map into the SDK metadata map,
// returning nil when unset so no metadata is sent.
func glossaryMetadata(m types.Map) map[string]any {
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

// applyComputedFields copies the server-generated (read-only) attributes from an
// API response onto the model, leaving every configured attribute untouched.
// Create and Update use this so plan values, including nulls, are saved to state
// exactly as written — only unknown (computed) values may change after apply.
func applyGlossaryComputedFields(model *GlossaryResourceModel, term *marmot.GlossaryTerm) {
	model.ID = types.StringValue(term.ID)
	model.CreatedAt = types.StringValue(term.CreatedAt)
	model.UpdatedAt = types.StringValue(term.UpdatedAt)
}

func (r *GlossaryResource) updateModelFromResponse(ctx context.Context, model *GlossaryResourceModel, term *marmot.GlossaryTerm) diag.Diagnostics {
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
