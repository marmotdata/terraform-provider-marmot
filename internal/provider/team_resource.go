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
var _ resource.Resource = &TeamResource{}
var _ resource.ResourceWithImportState = &TeamResource{}

func NewTeamResource() resource.Resource {
	return &TeamResource{}
}

// TeamResource defines the resource implementation.
type TeamResource struct {
	client *marmot.Client
}

// TeamResourceModel describes the team resource data model.
type TeamResourceModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Tags        types.Set    `tfsdk:"tags"`
	Metadata    types.Map    `tfsdk:"metadata"`
	ID          types.String `tfsdk:"id"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *TeamResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (r *TeamResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A team in Marmot. Put its `id` in the `owner_team_ids` of a data " +
			"product or glossary term to have it own that entity.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the team",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the team",
				Optional:            true,
			},
			"tags": schema.SetAttribute{
				MarkdownDescription: "Tags associated with the team",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"metadata": schema.MapAttribute{
				MarkdownDescription: "Key/value metadata for the team. The API can't clear metadata on " +
					"update: once set, removing every key leaves the old values in place until the team " +
					"is replaced.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Team ID",
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

func (r *TeamResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TeamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data TeamResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	team, err := r.client.Teams.Create(ctx, marmot.CreateTeamInput{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create team: %s", err))
		return
	}

	if team.ID == "" {
		resp.Diagnostics.AddError("API Error", "Team created but no ID returned")
		return
	}

	// The create endpoint accepts only name and description, so apply any tags
	// or metadata with a follow-up update.
	tags := dataProductTags(ctx, data.Tags, &resp.Diagnostics)
	metadata := dataProductMetadata(data.Metadata)
	if len(tags) > 0 || len(metadata) > 0 {
		team, err = r.client.Teams.Update(ctx, team.ID, marmot.UpdateTeamInput{
			Name:        data.Name.ValueString(),
			Description: data.Description.ValueString(),
			Tags:        tags,
			Metadata:    metadata,
		})
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to apply team tags/metadata: %s", err))
			return
		}
	}

	applyTeamComputedFields(&data, team)

	tflog.Info(ctx, "Team created", map[string]any{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TeamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data TeamResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	team, err := r.client.Teams.Get(ctx, data.ID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read team: %s", err))
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, team)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TeamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data TeamResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state TeamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	team, err := r.client.Teams.Update(ctx, state.ID.ValueString(), marmot.UpdateTeamInput{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
		Tags:        dataProductTagsOrEmpty(ctx, data.Tags, &resp.Diagnostics),
		Metadata:    dataProductMetadata(data.Metadata),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update team: %s", err))
		return
	}

	applyTeamComputedFields(&data, team)

	tflog.Info(ctx, "Team updated", map[string]any{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *TeamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data TeamResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Teams.Delete(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete team: %s", err))
		return
	}

	tflog.Info(ctx, "Team deleted", map[string]any{
		"id": data.ID.ValueString(),
	})
}

func (r *TeamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyTeamComputedFields copies the server-generated (read-only) attributes
// from an API response onto the model, leaving every configured attribute
// untouched.
func applyTeamComputedFields(model *TeamResourceModel, team *marmot.Team) {
	model.ID = types.StringValue(team.ID)
	model.CreatedAt = types.StringValue(team.CreatedAt)
	model.UpdatedAt = types.StringValue(team.UpdatedAt)
}

func (r *TeamResource) updateModelFromResponse(ctx context.Context, model *TeamResourceModel, team *marmot.Team) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(team.ID)
	model.Name = types.StringValue(team.Name)
	model.CreatedAt = types.StringValue(team.CreatedAt)
	model.UpdatedAt = types.StringValue(team.UpdatedAt)

	if team.Description != "" {
		model.Description = types.StringValue(team.Description)
	} else {
		model.Description = types.StringNull()
	}

	if len(team.Tags) > 0 {
		sortedTags := make([]string, len(team.Tags))
		copy(sortedTags, team.Tags)
		sort.Strings(sortedTags)

		tags, diag := types.SetValueFrom(ctx, types.StringType, sortedTags)
		diags.Append(diag...)
		model.Tags = tags
	} else {
		model.Tags = types.SetNull(types.StringType)
	}

	if metaMap, ok := team.Metadata.(map[string]any); ok && len(metaMap) > 0 {
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
