// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

var _ resource.Resource = &ServiceAccountAPIKeyResource{}

func NewServiceAccountAPIKeyResource() resource.Resource {
	return &ServiceAccountAPIKeyResource{}
}

type ServiceAccountAPIKeyResource struct {
	client *marmot.Client
}

type ServiceAccountAPIKeyResourceModel struct {
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	Name             types.String `tfsdk:"name"`
	ExpiresInDays    types.Int64  `tfsdk:"expires_in_days"`
	ExpiresAt        types.String `tfsdk:"expires_at"`
	ID               types.String `tfsdk:"id"`
}

func (r *ServiceAccountAPIKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account_api_key"
}

func (r *ServiceAccountAPIKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A durable API key slot on a service account. The server only " +
			"discloses a key's plaintext at creation and this resource deliberately does not " +
			"store it, so Terraform state stays free of credentials; obtain a usable key with " +
			"the ephemeral `marmot_service_account_api_key` instead, or mint durable keys " +
			"outside Terraform. Every attribute change replaces the key. Accounts are limited " +
			"to 5 keys.",

		Attributes: map[string]schema.Attribute{
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "ID of the service account the key belongs to",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the key, e.g. what system holds it",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"expires_in_days": schema.Int64Attribute{
				MarkdownDescription: "Days until the key expires. Omit for a key that never expires.",
				Optional:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "Expiry timestamp, if the key expires",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Key ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ServiceAccountAPIKeyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ServiceAccountAPIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceAccountAPIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.ServiceAccounts.CreateAPIKey(ctx, data.ServiceAccountID.ValueString(),
		marmot.CreateServiceAccountAPIKeyInput{
			Name:          data.Name.ValueString(),
			ExpiresInDays: data.ExpiresInDays.ValueInt64(),
		})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create API key", err.Error())
		return
	}

	data.ID = types.StringValue(key.ID)
	data.ExpiresAt = types.StringValue(key.ExpiresAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceAccountAPIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceAccountAPIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := r.client.ServiceAccounts.ListAPIKeys(ctx, data.ServiceAccountID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to list API keys", err.Error())
		return
	}
	for _, key := range keys {
		if key.ID == data.ID.ValueString() {
			data.ExpiresAt = types.StringValue(key.ExpiresAt)
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *ServiceAccountAPIKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every attribute requires replacement; Update never runs with changes.
	resp.Diagnostics.Append(resp.State.Set(ctx, req.Plan.Raw)...)
}

func (r *ServiceAccountAPIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServiceAccountAPIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.ServiceAccounts.DeleteAPIKey(ctx,
		data.ServiceAccountID.ValueString(), data.ID.ValueString())
	if err != nil && !marmot.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete API key", err.Error())
	}
}
