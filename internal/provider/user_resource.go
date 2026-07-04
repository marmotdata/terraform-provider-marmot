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
var _ resource.Resource = &UserResource{}
var _ resource.ResourceWithImportState = &UserResource{}

func NewUserResource() resource.Resource {
	return &UserResource{}
}

// UserResource defines the resource implementation.
type UserResource struct {
	client *marmot.Client
}

// UserResourceModel describes the user resource data model.
type UserResourceModel struct {
	Name              types.String `tfsdk:"name"`
	Username          types.String `tfsdk:"username"`
	PasswordWO        types.String `tfsdk:"password_wo"`
	PasswordWOVersion types.String `tfsdk:"password_wo_version"`
	RoleNames         types.Set    `tfsdk:"role_names"`
	ProfilePicture    types.String `tfsdk:"profile_picture"`
	Active            types.Bool   `tfsdk:"active"`
	ID                types.String `tfsdk:"id"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func (r *UserResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A user account in Marmot. Put its `id` in the `owner_user_ids` of a " +
			"data product or glossary term to have it own that entity.\n\n" +
			"Set the password through the write-only `password_wo` attribute so it never lands in " +
			"Terraform state. This needs Terraform 1.11 or newer.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the user",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Login username. Changing this forces a new user to be created.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password_wo": schema.StringAttribute{
				MarkdownDescription: "The user's password. Write-only, so it is never kept in state or " +
					"plan. Bump `password_wo_version` to push a new value on update.",
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
			},
			"password_wo_version": schema.StringAttribute{
				MarkdownDescription: "A version marker for `password_wo`. Terraform can't diff a " +
					"write-only value, so changing this is what tells it to send the current " +
					"`password_wo` on the next apply.",
				Optional: true,
			},
			"role_names": schema.SetAttribute{
				MarkdownDescription: "Names of the roles assigned to the user",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
			},
			"profile_picture": schema.StringAttribute{
				MarkdownDescription: "URL of the user's profile picture",
				Optional:            true,
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the user account is active",
				Computed:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "User ID",
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

func (r *UserResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data UserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Write-only values are null in the plan and must be read from the config.
	var password types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("password_wo"), &password)...)
	if resp.Diagnostics.HasError() {
		return
	}

	user, err := r.client.Users.Create(ctx, marmot.CreateUserInput{
		Name:           data.Name.ValueString(),
		Username:       data.Username.ValueString(),
		Password:       password.ValueString(),
		RoleNames:      userRoleNames(ctx, data.RoleNames, &resp.Diagnostics),
		ProfilePicture: data.ProfilePicture.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create user: %s", err))
		return
	}

	if user.ID == "" {
		resp.Diagnostics.AddError("API Error", "User created but no ID returned")
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, user)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "User created", map[string]any{
		"id":       data.ID.ValueString(),
		"username": data.Username.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data UserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	user, err := r.client.Users.Get(ctx, data.ID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read user: %s", err))
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, user)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data UserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only send a new password when password_wo_version changes, since the
	// write-only value itself cannot be compared against prior state.
	var password string
	if !data.PasswordWOVersion.Equal(state.PasswordWOVersion) {
		var pw types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("password_wo"), &pw)...)
		if resp.Diagnostics.HasError() {
			return
		}
		password = pw.ValueString()
	}

	user, err := r.client.Users.Update(ctx, state.ID.ValueString(), marmot.UpdateUserInput{
		Name:           data.Name.ValueString(),
		Password:       password,
		RoleNames:      userRoleNames(ctx, data.RoleNames, &resp.Diagnostics),
		ProfilePicture: data.ProfilePicture.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update user: %s", err))
		return
	}

	diags := r.updateModelFromResponse(ctx, &data, user)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "User updated", map[string]any{
		"id":       data.ID.ValueString(),
		"username": data.Username.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data UserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Users.Delete(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete user: %s", err))
		return
	}

	tflog.Info(ctx, "User deleted", map[string]any{
		"id": data.ID.ValueString(),
	})
}

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// userRoleNames converts the Terraform role_names set into a sorted string
// slice, returning nil when unset.
func userRoleNames(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var roles []string
	diags.Append(set.ElementsAs(ctx, &roles, false)...)
	sort.Strings(roles)
	return roles
}

func (r *UserResource) updateModelFromResponse(ctx context.Context, model *UserResourceModel, user *marmot.User) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(user.ID)
	model.Name = types.StringValue(user.Name)
	model.Username = types.StringValue(user.Username)
	model.Active = types.BoolValue(user.Active)
	model.CreatedAt = types.StringValue(user.CreatedAt)
	model.UpdatedAt = types.StringValue(user.UpdatedAt)

	if user.ProfilePicture != "" {
		model.ProfilePicture = types.StringValue(user.ProfilePicture)
	} else {
		model.ProfilePicture = types.StringNull()
	}

	if len(user.Roles) > 0 {
		names := make([]string, 0, len(user.Roles))
		for _, role := range user.Roles {
			if role != nil {
				names = append(names, role.Name)
			}
		}
		sort.Strings(names)

		roles, diag := types.SetValueFrom(ctx, types.StringType, names)
		diags.Append(diag...)
		model.RoleNames = roles
	} else {
		model.RoleNames = types.SetNull(types.StringType)
	}

	return diags
}
