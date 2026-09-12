// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

var _ resource.Resource = &ServiceAccountLeaseResource{}
var _ resource.ResourceWithImportState = &ServiceAccountLeaseResource{}

func NewServiceAccountLeaseResource() resource.Resource {
	return &ServiceAccountLeaseResource{}
}

type ServiceAccountLeaseResource struct {
	client *secretStoreClient
}

type ServiceAccountLeaseResourceModel struct {
	ServiceAccountID types.String         `tfsdk:"service_account_id"`
	Store            types.String         `tfsdk:"store"`
	Ref              jsontypes.Normalized `tfsdk:"ref"`
	TTLSeconds       types.Int64          `tfsdk:"ttl_seconds"`
	ID               types.String         `tfsdk:"id"`
	CurrentKeyID     types.String         `tfsdk:"current_key_id"`
	PreviousKeyID    types.String         `tfsdk:"previous_key_id"`
	LeasedAt         types.String         `tfsdk:"leased_at"`
	ExpiresAt        types.String         `tfsdk:"expires_at"`
	LastError        types.String         `tfsdk:"last_error"`
}

func (r *ServiceAccountLeaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account_lease"
}

func (r *ServiceAccountLeaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: secretStoreCloudOnly + "A lease replaces a durable API key: Marmot mints " +
			"a short-lived key for the service account, writes it to a secret store at `ref`, and " +
			"renews it at half the TTL. The agent reads the key from the store with its own identity, " +
			"so nothing long-lived is handed out and nothing secret enters Terraform state. An account " +
			"holds at most one lease; the store must support writes (every built-in type does).\n\n" +
			"The first key is written before the lease is created: a ref the store cannot write to " +
			"fails the apply with the store's error, and no lease is left behind.",

		Attributes: map[string]schema.Attribute{
			"service_account_id": schema.StringAttribute{
				MarkdownDescription: "ID of the service account the lease belongs to",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"store": schema.StringAttribute{
				MarkdownDescription: "ID of the `marmot_secret_store_*` resource the key is written through",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"ref": schema.StringAttribute{
				MarkdownDescription: "Where in the store the key is written, as a JSON object whose keys " +
					"depend on the store type; see the store resource. Use `jsonencode()` to build it " +
					"from HCL.",
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
			},
			"ttl_seconds": schema.Int64Attribute{
				MarkdownDescription: "Lifetime of each minted key, between 300 and 86400 seconds. " +
					"Defaults to 3600. The key is renewed at half this.",
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(3600),
				Validators: []validator.Int64{
					int64validator.Between(300, 86400),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Lease ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"current_key_id": schema.StringAttribute{
				MarkdownDescription: "ID of the API key currently written to the store",
				Computed:            true,
			},
			"previous_key_id": schema.StringAttribute{
				MarkdownDescription: "ID of the key the current one replaced. It stays valid until " +
					"its own expiry so an agent that read the store just before a renewal keeps " +
					"working, and is revoked at the next renewal. Empty until the first renewal.",
				Computed: true,
			},
			"leased_at": schema.StringAttribute{
				MarkdownDescription: "When the current key was written",
				Computed:            true,
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "When the current key expires; renewal happens before this",
				Computed:            true,
			},
			"last_error": schema.StringAttribute{
				MarkdownDescription: "Why the most recent renewal failed, empty when it succeeded",
				Computed:            true,
			},
		},
	}
}

func (r *ServiceAccountLeaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *marmot.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = newSecretStoreClient(client)
}

// leaseRequest turns the configured attributes into the body the API takes.
func leaseRequest(data *ServiceAccountLeaseResourceModel) (setLeaseRequest, diag.Diagnostics) {
	var ref map[string]any
	diags := data.Ref.Unmarshal(&ref)
	return setLeaseRequest{
		SecretStoreID: data.Store.ValueString(),
		Ref:           ref,
		TTLSeconds:    data.TTLSeconds.ValueInt64(),
	}, diags
}

// applyLeaseComputedFields copies the server-generated attributes onto the
// model, leaving the configured ones as written.
func applyLeaseComputedFields(data *ServiceAccountLeaseResourceModel, lease *serviceAccountLease) {
	data.ID = types.StringValue(lease.ID)
	data.TTLSeconds = types.Int64Value(lease.TTLSeconds)
	data.CurrentKeyID = types.StringValue(lease.CurrentKeyID)
	data.PreviousKeyID = types.StringValue(lease.PreviousKeyID)
	data.LeasedAt = types.StringValue(lease.LeasedAt)
	data.ExpiresAt = types.StringValue(lease.ExpiresAt)
	data.LastError = types.StringValue(lease.LastError)
}

func (r *ServiceAccountLeaseResource) apply(ctx context.Context, data *ServiceAccountLeaseResourceModel, diags *diag.Diagnostics) {
	in, d := leaseRequest(data)
	diags.Append(d...)
	if diags.HasError() {
		return
	}

	lease, err := r.client.SetLease(ctx, data.ServiceAccountID.ValueString(), in)
	if err != nil {
		diags.AddError("Unable to Set Service Account Lease", err.Error())
		return
	}

	applyLeaseComputedFields(data, lease)

	tflog.Info(ctx, "Service account lease set", map[string]any{
		"id":                 lease.ID,
		"service_account_id": lease.ServiceAccountID,
		"secret_store_id":    lease.SecretStoreID,
	})
}

func (r *ServiceAccountLeaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServiceAccountLeaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceAccountLeaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServiceAccountLeaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	lease, err := r.client.GetLease(ctx, data.ServiceAccountID.ValueString())
	if err != nil {
		if errors.Is(err, errNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Service Account Lease", err.Error())
		return
	}

	data.Store = types.StringValue(lease.SecretStoreID)
	encoded, err := json.Marshal(lease.Ref)
	if err != nil {
		resp.Diagnostics.AddError("Ref Error", fmt.Sprintf("Unable to encode lease ref: %s", err))
		return
	}
	data.Ref = jsontypes.NewNormalizedValue(string(encoded))
	applyLeaseComputedFields(&data, lease)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update repoints the one lease the account has: the server upserts on PUT,
// so a changed store, ref or TTL is a new first renewal, not a new lease.
func (r *ServiceAccountLeaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServiceAccountLeaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceAccountLeaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServiceAccountLeaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A lease already gone is the outcome Delete wanted.
	err := r.client.DeleteLease(ctx, data.ServiceAccountID.ValueString())
	if err != nil && !errors.Is(err, errNotFound) {
		resp.Diagnostics.AddError("Unable to Delete Service Account Lease", err.Error())
		return
	}

	tflog.Info(ctx, "Service account lease deleted", map[string]any{
		"service_account_id": data.ServiceAccountID.ValueString(),
	})
}

// ImportState takes the service account id: an account has one lease, and the
// lease endpoints are addressed by the account.
func (r *ServiceAccountLeaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("service_account_id"), req, resp)
}
