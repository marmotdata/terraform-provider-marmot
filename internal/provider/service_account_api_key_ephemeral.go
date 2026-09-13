// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

var _ ephemeral.EphemeralResource = &ServiceAccountAPIKeyEphemeralResource{}
var _ ephemeral.EphemeralResourceWithClose = &ServiceAccountAPIKeyEphemeralResource{}
var _ ephemeral.EphemeralResourceWithConfigure = &ServiceAccountAPIKeyEphemeralResource{}

func NewServiceAccountAPIKeyEphemeralResource() ephemeral.EphemeralResource {
	return &ServiceAccountAPIKeyEphemeralResource{}
}

type ServiceAccountAPIKeyEphemeralResource struct {
	client *marmot.Client
}

type serviceAccountAPIKeyEphemeralModel struct {
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	Name             types.String `tfsdk:"name"`
	ExpiresInDays    types.Int64  `tfsdk:"expires_in_days"`
	Key              types.String `tfsdk:"key"`
	ExpiresAt        types.String `tfsdk:"expires_at"`
}

// ephemeralKeyCleanup is carried in private state from Open to Close.
type ephemeralKeyCleanup struct {
	ServiceAccountID string `json:"service_account_id"`
	KeyID            string `json:"key_id"`
}

func (r *ServiceAccountAPIKeyEphemeralResource) Metadata(_ context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account_api_key"
}

func (r *ServiceAccountAPIKeyEphemeralResource) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A service account API key that lives for one Terraform operation: " +
			"created on open, revoked on close, never in plan or state. For a durable key use the " +
			"`marmot_service_account_api_key` resource.",

		Attributes: map[string]schema.Attribute{
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "ID of the service account to create the key for.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Key name. Defaults to `terraform-ephemeral`.",
				Optional:            true,
			},
			"expires_in_days": schema.Int64Attribute{
				MarkdownDescription: "Days until the key expires. Defaults to 1.",
				Optional:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The plaintext API key.",
				Computed:            true,
				Sensitive:           true,
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "Expiry timestamp of the created key.",
				Computed:            true,
			},
		},
	}
}

func (r *ServiceAccountAPIKeyEphemeralResource) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data",
			fmt.Sprintf("Expected *marmot.Client, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *ServiceAccountAPIKeyEphemeralResource) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Provider Not Configured",
			"The Marmot client was not configured before Open.")
		return
	}
	var data serviceAccountAPIKeyEphemeralModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	if name == "" {
		name = "terraform-ephemeral"
	}
	expires := data.ExpiresInDays.ValueInt64()
	if data.ExpiresInDays.IsNull() {
		expires = 1
	}

	key, err := r.client.ServiceAccounts.CreateAPIKey(ctx, data.ServiceAccountID.ValueString(),
		marmot.CreateServiceAccountAPIKeyInput{
			Name:          name,
			ExpiresInDays: expires,
		})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create ephemeral API key", err.Error())
		return
	}

	// The result may only differ from config in computed attributes.
	data.Key = types.StringValue(key.Key)
	data.ExpiresAt = types.StringValue(key.ExpiresAt)
	resp.Diagnostics.Append(resp.Result.Set(ctx, &data)...)

	cleanup, err := json.Marshal(ephemeralKeyCleanup{
		ServiceAccountID: data.ServiceAccountID.ValueString(),
		KeyID:            key.ID,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to record key for cleanup", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, "cleanup", cleanup)...)
}

func (r *ServiceAccountAPIKeyEphemeralResource) Close(ctx context.Context, req ephemeral.CloseRequest, resp *ephemeral.CloseResponse) {
	raw, diags := req.Private.GetKey(ctx, "cleanup")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() || len(raw) == 0 {
		return
	}
	var cleanup ephemeralKeyCleanup
	if err := json.Unmarshal(raw, &cleanup); err != nil {
		resp.Diagnostics.AddError("Failed to decode key cleanup data", err.Error())
		return
	}
	err := r.client.ServiceAccounts.DeleteAPIKey(ctx, cleanup.ServiceAccountID, cleanup.KeyID)
	if err != nil && !marmot.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to revoke ephemeral API key",
			fmt.Sprintf("Key %s on service account %s was not revoked, remove it by hand: %s",
				cleanup.KeyID, cleanup.ServiceAccountID, err.Error()))
	}
}
