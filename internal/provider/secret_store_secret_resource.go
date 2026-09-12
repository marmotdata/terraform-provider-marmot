// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// secretRefField is one key of a secret's ref, exposed as an attribute of
// the same name. The server validates the ref against the store type and
// stores it as sent, so a default the server would apply is mirrored here
// and always goes on the wire.
type secretRefField struct {
	name          string
	description   string
	required      bool
	serverDefault string
	// integer marks an int64 attribute; the rest are strings.
	integer bool
}

func (f secretRefField) attribute() schema.Attribute {
	if f.integer {
		return schema.Int64Attribute{
			MarkdownDescription: f.description,
			Required:            f.required,
			Optional:            !f.required,
		}
	}
	a := schema.StringAttribute{
		MarkdownDescription: f.description,
		Required:            f.required,
		Optional:            !f.required,
	}
	if f.required {
		a.Validators = []validator.String{stringvalidator.LengthAtLeast(1)}
	}
	if f.serverDefault != "" {
		a.MarkdownDescription += " Defaults to `" + f.serverDefault + "`."
		a.Computed = true
		a.Default = stringdefault.StaticString(f.serverDefault)
	}
	return a
}

// secretKind describes one store type's secrets as a Terraform resource.
// Every type shares the CRUD below over the generic secrets API; only the
// ref's shape differs.
type secretKind struct {
	storeType   string
	label       string
	description string
	fields      []secretRefField
}

var secretKinds = []secretKind{
	{
		storeType:   "google",
		label:       "Google Secret Manager",
		description: "A secret in a Google Secret Manager store: a secret and a version in a project.",
		fields: []secretRefField{
			{name: "project", description: "Project ID or number the secret lives in.", required: true},
			{name: "location", description: "Region of a regional secret, for example `europe-west1`. Omit for a global secret."},
			{name: "secret_id", description: "Name of the secret.", required: true},
			{name: "version", description: "Version to read.", serverDefault: "latest"},
		},
	},
	{
		storeType:   "aws",
		label:       "AWS Secrets Manager",
		description: "A secret in an AWS Secrets Manager store, by name or ARN.",
		fields: []secretRefField{
			{name: "region", description: "Region the secret lives in. Required unless `secret_id` is an ARN, which carries its own."},
			{name: "secret_id", description: "Name or ARN of the secret.", required: true},
			{name: "version_stage", description: "Staging label to read.", serverDefault: "AWSCURRENT"},
			{name: "version_id", description: "Version to read. Pins reads to that version; a lease cannot write to a pinned version."},
		},
	},
	{
		storeType:   "azure",
		label:       "Azure Key Vault",
		description: "A secret in an Azure Key Vault store.",
		fields: []secretRefField{
			{name: "vault_uri", description: "Key Vault URI, for example `https://my-vault.vault.azure.net`.", required: true},
			{name: "name", description: "Name of the secret.", required: true},
			{name: "version", description: "Version to read. Omit to read the latest."},
		},
	},
	{
		storeType:   "vault",
		label:       "HashiCorp Vault",
		description: "A secret in a HashiCorp Vault KV v2 store: a key of a secret under a mount.",
		fields: []secretRefField{
			{name: "mount", description: "KV v2 mount path.", serverDefault: "secret"},
			{name: "name", description: "Path of the secret under the mount.", required: true},
			{name: "key", description: "Key inside the secret. Omit when the secret holds exactly one."},
			{name: "version", description: "Version to read. Omit to read the latest.", integer: true},
		},
	},
}

// SecretStoreSecretResources returns one secret resource per store type.
func SecretStoreSecretResources() []func() resource.Resource {
	out := make([]func() resource.Resource, 0, len(secretKinds))
	for _, kind := range secretKinds {
		out = append(out, func() resource.Resource {
			return &secretResource{kind: kind}
		})
	}
	return out
}

type secretResource struct {
	kind   secretKind
	client *secretStoreClient
}

var _ resource.Resource = &secretResource{}
var _ resource.ResourceWithImportState = &secretResource{}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret_store_" + r.kind.storeType + "_secret"
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data",
			fmt.Sprintf("Expected *marmot.Client, got %T", req.ProviderData))
		return
	}
	r.client = newSecretStoreClient(client)
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"store": schema.StringAttribute{
			MarkdownDescription: "ID of the `marmot_secret_store_" + r.kind.storeType + "` the secret lives in. " +
				"Changing it replaces the secret.",
			Required: true,
			Validators: []validator.String{
				stringvalidator.LengthAtLeast(1),
			},
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"id": schema.StringAttribute{
			MarkdownDescription: "Secret ID, what a `marmot_pipeline` or `marmot_service_account_lease` references",
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
	}
	for _, f := range r.kind.fields {
		attrs[f.name] = f.attribute()
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: secretStoreCloudOnly + r.kind.description +
			"\n\nOnly the location is registered; the value is read from " + r.kind.label +
			" when a `marmot_pipeline` runs, and never enters Terraform state. Repointing the " +
			"secret updates it in place and every pipeline and lease that references it follows. " +
			"Registering secrets requires the `secretStore:use` permission.",
		Attributes: attrs,
	}
}

// refFrom assembles the ref the API takes from the planned attributes. Null
// attributes are left out.
func (r *secretResource) refFrom(ctx context.Context, src attrGetter, diags *diag.Diagnostics) map[string]any {
	ref := make(map[string]any, len(r.kind.fields))
	for _, f := range r.kind.fields {
		if f.integer {
			var v types.Int64
			diags.Append(src.GetAttribute(ctx, path.Root(f.name), &v)...)
			if !v.IsNull() && !v.IsUnknown() {
				ref[f.name] = v.ValueInt64()
			}
			continue
		}
		var v types.String
		diags.Append(src.GetAttribute(ctx, path.Root(f.name), &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			ref[f.name] = v.ValueString()
		}
	}
	return ref
}

// persist writes a secret back to state as the server holds it.
func (r *secretResource) persist(ctx context.Context, state *tfsdk.State, secret *secretStoreSecret, diags *diag.Diagnostics) {
	diags.Append(state.SetAttribute(ctx, path.Root("id"), secret.ID)...)
	diags.Append(state.SetAttribute(ctx, path.Root("store"), secret.SecretStoreID)...)
	for _, f := range r.kind.fields {
		raw := secret.Ref[f.name]
		if f.integer {
			// JSON numbers decode as float64; an absent key reads as null.
			v := types.Int64Null()
			if n, ok := raw.(float64); ok {
				v = types.Int64Value(int64(n))
			}
			diags.Append(state.SetAttribute(ctx, path.Root(f.name), v)...)
			continue
		}
		diags.Append(state.SetAttribute(ctx, path.Root(f.name), fieldValue(raw))...)
	}
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var store types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("store"), &store)...)
	ref := r.refFrom(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	secret, err := r.client.CreateSecret(ctx, store.ValueString(), ref)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Register Secret", err.Error())
		return
	}

	tflog.Info(ctx, "Secret registered", map[string]any{
		"id":    secret.ID,
		"store": secret.SecretStoreID,
	})

	r.persist(ctx, &resp.State, secret, &resp.Diagnostics)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var store, id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("store"), &store)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secret, err := r.client.GetSecret(ctx, store.ValueString(), id.ValueString())
	if err != nil {
		if errors.Is(err, errNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Secret", err.Error())
		return
	}

	r.persist(ctx, &resp.State, secret, &resp.Diagnostics)
}

func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var store, id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("store"), &store)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	ref := r.refFrom(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	secret, err := r.client.UpdateSecret(ctx, store.ValueString(), id.ValueString(), ref)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Secret", err.Error())
		return
	}

	tflog.Info(ctx, "Secret updated", map[string]any{
		"id":    secret.ID,
		"store": secret.SecretStoreID,
	})

	r.persist(ctx, &resp.State, secret, &resp.Diagnostics)
}

func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var store, id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("store"), &store)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A secret already gone is the outcome Delete wanted. One still
	// referenced by a pipeline or a lease is refused, and stays.
	if err := r.client.DeleteSecret(ctx, store.ValueString(), id.ValueString()); err != nil && !errors.Is(err, errNotFound) {
		resp.Diagnostics.AddError("Unable to Delete Secret", err.Error())
		return
	}

	tflog.Info(ctx, "Secret deleted", map[string]any{
		"id":    id.ValueString(),
		"store": store.ValueString(),
	})
}

// ImportState takes "<store id>/<secret id>": the secret endpoints are
// addressed by both.
func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	store, id, ok := strings.Cut(req.ID, "/")
	if !ok || store == "" || id == "" {
		resp.Diagnostics.AddError("Invalid Import ID",
			fmt.Sprintf("Expected \"<store id>/<secret id>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("store"), store)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
