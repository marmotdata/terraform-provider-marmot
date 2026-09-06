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

// The server discloses an API key's plaintext exactly once, at creation, so
// "read the key of an existing slot" cannot exist. What can exist is a key
// whose whole life is one Terraform operation: minted on Open, revoked on
// Close, never written to plan or state. That is what this ephemeral resource
// is; the managed marmot_service_account_api_key handles durable key slots
// and never exposes plaintext.

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
		MarkdownDescription: "A service-account API key that lives for one Terraform operation: " +
			"created when the configuration is opened, revoked when it closes, and never stored " +
			"in plan or state. Use it to hand short-lived credentials to other providers or to " +
			"write-only attributes. The server only discloses a key's plaintext at creation, so " +
			"this is the way to obtain a usable key inside Terraform; the managed " +
			"`marmot_service_account_api_key` resource manages durable key slots without ever " +
			"exposing their plaintext.",

		Attributes: map[string]schema.Attribute{
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "ID of the service account to mint the key for.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name recorded for the key. Defaults to `terraform-ephemeral`. " +
					"Keys count toward the account's 5-key limit while the operation runs.",
				Optional: true,
			},
			"expires_in_days": schema.Int64Attribute{
				MarkdownDescription: "Days until the key expires server-side. Defaults to 1, so a " +
					"key orphaned by an interrupted run dies on its own.",
				Optional: true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The plaintext API key.",
				Computed:            true,
				Sensitive:           true,
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "Expiry timestamp of the minted key.",
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
			"The Marmot client was not configured before Open; this is a bug in the provider.")
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
		resp.Diagnostics.AddError("Failed to mint ephemeral API key", err.Error())
		return
	}

	// Only computed attributes may differ from configuration in the result;
	// the applied defaults stay internal.
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
			fmt.Sprintf("Key %s on service account %s could not be revoked and should be removed "+
				"by hand (it expires on its own): %s", cleanup.KeyID, cleanup.ServiceAccountID, err.Error()))
	}
}
