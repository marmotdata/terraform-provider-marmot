// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

var _ resource.Resource = &ServiceAccountResource{}
var _ resource.ResourceWithImportState = &ServiceAccountResource{}

func NewServiceAccountResource() resource.Resource {
	return &ServiceAccountResource{}
}

type ServiceAccountResource struct {
	client *marmot.Client
}

type ServiceAccountResourceModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Active      types.Bool   `tfsdk:"active"`
	RoleIDs     types.Set    `tfsdk:"role_ids"`
	ID          types.String `tfsdk:"id"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *ServiceAccountResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (r *ServiceAccountResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A machine principal. Grant it access either catalog-wide through " +
			"`role_ids` or per resource with the `*_iam_member` and `*_iam_binding` resources, " +
			"where it is referenced as `serviceAccount:{id}`. An account with no roles and no " +
			"grants can authenticate but reaches nothing.\n\nAPI keys are issued through the UI " +
			"or the API rather than by Terraform, since a key's plaintext is disclosed only at " +
			"creation and has no place in state. Keep them in a secret manager and read them " +
			"back as ephemeral values.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the service account",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the account is for and who owns it",
				Optional:            true,
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the account may authenticate. Defaults to true.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"role_ids": schema.SetAttribute{
				MarkdownDescription: "IDs of organization-level roles held by this account. These " +
					"apply across the whole catalog; use the IAM resources for per-resource grants.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Service account ID",
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
		},
	}
}

func (r *ServiceAccountResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *marmot.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *ServiceAccountResource) roleIDs(ctx context.Context, set types.Set, diags *resource.CreateResponse) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var ids []string
	_ = set.ElementsAs(ctx, &ids, false)
	return ids
}

func (r *ServiceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	account, err := r.client.ServiceAccounts.Create(ctx, marmot.CreateServiceAccountInput{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
		RoleIDs:     r.roleIDs(ctx, data.RoleIDs, resp),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create service account", err.Error())
		return
	}

	data.ID = types.StringValue(account.ID)
	data.CreatedAt = types.StringValue(account.CreatedAt)
	data.Active = types.BoolValue(account.Active)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	account, err := r.client.ServiceAccounts.Get(ctx, data.ID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read service account", err.Error())
		return
	}

	data.Name = types.StringValue(account.Name)
	if account.Description != "" || !data.Description.IsNull() {
		data.Description = types.StringValue(account.Description)
	}
	data.Active = types.BoolValue(account.Active)
	if !data.RoleIDs.IsNull() {
		ids := make([]string, 0, len(account.Roles))
		for _, role := range account.Roles {
			ids = append(ids, role.ID)
		}
		set, diags := types.SetValueFrom(ctx, types.StringType, ids)
		resp.Diagnostics.Append(diags...)
		data.RoleIDs = set
	}
	data.CreatedAt = types.StringValue(account.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServiceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var ids []string
	if !data.RoleIDs.IsNull() && !data.RoleIDs.IsUnknown() {
		resp.Diagnostics.Append(data.RoleIDs.ElementsAs(ctx, &ids, false)...)
	}
	if ids == nil {
		ids = []string{}
	}

	account, err := r.client.ServiceAccounts.Update(ctx, data.ID.ValueString(),
		marmot.UpdateServiceAccountInput{
			Name:        data.Name.ValueString(),
			Description: data.Description.ValueString(),
			Active:      data.Active.ValueBool(),
			RoleIDs:     ids,
		})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update service account", err.Error())
		return
	}

	data.Active = types.BoolValue(account.Active)
	data.CreatedAt = types.StringValue(account.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServiceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.ServiceAccounts.Delete(ctx, data.ID.ValueString()); err != nil && !marmot.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete service account", err.Error())
	}
}

func (r *ServiceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
