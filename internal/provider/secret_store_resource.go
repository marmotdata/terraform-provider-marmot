// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

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
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// secretStoreField is one key of a store's config, exposed as an attribute.
type secretStoreField struct {
	name        string
	description string
	required    bool
	// serverDefault mirrors the server's default so the plan knows it.
	serverDefault string
	// derivedFrom names the keys the server derives this value from when it
	// is unset. The plan keeps the stored value until one of them changes.
	derivedFrom []string
}

func (f secretStoreField) attribute() schema.Attribute {
	a := schema.StringAttribute{
		MarkdownDescription: f.description,
		Required:            f.required,
		Optional:            !f.required,
	}
	if f.serverDefault != "" {
		a.MarkdownDescription += " Defaults to `" + f.serverDefault + "`."
		a.Computed = true
		a.Default = stringdefault.StaticString(f.serverDefault)
	}
	if len(f.derivedFrom) > 0 {
		a.Computed = true
		a.PlanModifiers = []planmodifier.String{rootPaths(f.derivedFrom...)}
	}
	return a
}

// keepStateUnless plans the stored value of a server-filled attribute as
// long as none of the attributes it derives from change.
type keepStateUnless []path.Path

var _ planmodifier.String = keepStateUnless(nil)

func rootPaths(names ...string) keepStateUnless {
	out := make(keepStateUnless, 0, len(names))
	for _, name := range names {
		out = append(out, path.Root(name))
	}
	return out
}

func (m keepStateUnless) Description(ctx context.Context) string {
	return m.MarkdownDescription(ctx)
}

func (m keepStateUnless) MarkdownDescription(context.Context) string {
	return "Keeps the stored value unless an attribute it is derived from changes."
}

func (m keepStateUnless) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.PlanValue.IsUnknown() || req.State.Raw.IsNull() {
		return
	}
	for _, p := range m {
		before, ok := valueAt(req.State.Raw, p)
		if !ok {
			return
		}
		after, ok := valueAt(req.Plan.Raw, p)
		if !ok || !after.IsFullyKnown() || !after.Equal(before) {
			return
		}
	}
	resp.PlanValue = req.StateValue
}

// valueAt reads the value at an attribute path of a raw plan or state.
func valueAt(root tftypes.Value, p path.Path) (tftypes.Value, bool) {
	tfPath := tftypes.NewAttributePath()
	for _, step := range p.Steps() {
		name, ok := step.(path.PathStepAttributeName)
		if !ok {
			return tftypes.Value{}, false
		}
		tfPath = tfPath.WithAttributeName(string(name))
	}
	v, _, err := tftypes.WalkAttributePath(root, tfPath)
	if err != nil {
		return tftypes.Value{}, false
	}
	value, ok := v.(tftypes.Value)
	return value, ok
}

// secretStoreKind describes one store type. All types share the CRUD below;
// only the schema differs.
type secretStoreKind struct {
	// storeType is the server's id for the type and the resource name suffix.
	storeType   string
	label       string
	description string
	// federation names the keys that, when set, make the store federate.
	federation []string
	fields     []secretStoreField
}

func audienceField(description string, derivedFrom ...string) secretStoreField {
	return secretStoreField{
		name:        "audience",
		description: "Audience of the Marmot token. " + description + " Only used by a federated store.",
		derivedFrom: derivedFrom,
	}
}

var secretStoreKinds = []secretStoreKind{
	{
		storeType: "google",
		label:     "Google Secret Manager",
		description: "A Google Secret Manager store. Set `workload_identity_provider` to federate; " +
			"without it the server reads with its own Application Default Credentials.",
		federation: []string{"workload_identity_provider"},
		fields: []secretStoreField{
			{
				name: "workload_identity_provider",
				description: "Workload Identity Federation provider to exchange the Marmot token at, " +
					"`projects/{number}/locations/global/workloadIdentityPools/{pool}/providers/{provider}`. " +
					"Setting it federates the store.",
			},
			{
				name: "service_account",
				description: "Service account to impersonate after the exchange. Empty reads as " +
					"the federated principal.",
			},
			audienceField("Derived from `workload_identity_provider` by the server when unset.",
				"workload_identity_provider"),
		},
	},
	{
		storeType: "aws",
		label:     "AWS Secrets Manager",
		description: "An AWS Secrets Manager store. Set `role_arn` to federate; without it the " +
			"server reads with its own credential chain.",
		federation: []string{"role_arn"},
		fields: []secretStoreField{
			{
				name: "role_arn",
				description: "Role to assume with the Marmot token. Its trust policy must name the " +
					"Marmot issuer as an OIDC provider. Setting it federates the store.",
			},
			{
				name:          "session_name",
				description:   "Session name of the assumed role.",
				serverDefault: "marmot",
			},
			audienceField("The OIDC provider's client ID. The server sets `sts.amazonaws.com` "+
				"when unset.", "role_arn"),
		},
	},
	{
		storeType: "azure",
		label:     "Azure Key Vault",
		description: "An Azure Key Vault store. Set `tenant_id` and `client_id` to federate; " +
			"without them the server reads with its own `DefaultAzureCredential`.",
		federation: []string{"tenant_id", "client_id"},
		fields: []secretStoreField{
			{
				name: "tenant_id",
				description: "Tenant of the app registration. Set with `client_id` to federate " +
					"the store.",
			},
			{
				name: "client_id",
				description: "App registration (client) ID whose federated credential trusts the " +
					"Marmot issuer. Set with `tenant_id`.",
			},
			audienceField("The federated credential's audience. The server sets "+
				"`api://AzureADTokenExchange` when unset.", "tenant_id", "client_id"),
		},
	},
	{
		storeType: "vault",
		label:     "HashiCorp Vault",
		description: "A HashiCorp Vault KV v2 store. Set `role` to federate; without it the " +
			"server logs in with its own `VAULT_TOKEN`.",
		federation: []string{"role"},
		fields: []secretStoreField{
			{
				name:        "address",
				description: "Vault server URL.",
				required:    true,
			},
			{name: "namespace", description: "Vault Enterprise namespace."},
			{
				name: "ca_cert",
				description: "PEM CA certificate that signed the Vault server's TLS certificate. " +
					"Omit to use the system roots.",
			},
			{
				name: "role",
				description: "JWT auth role to log in as with the Marmot token. Setting it " +
					"federates the store.",
			},
			{
				name:          "auth_path",
				description:   "Mount path of the JWT auth method.",
				serverDefault: "jwt",
			},
			audienceField("The JWT role's `bound_audiences`. The server sets the Vault "+
				"address when unset.", "role", "address"),
		},
	},
}

// SecretStoreResources returns one resource per store type.
func SecretStoreResources() []func() resource.Resource {
	out := make([]func() resource.Resource, 0, len(secretStoreKinds))
	for _, kind := range secretStoreKinds {
		out = append(out, func() resource.Resource {
			return &secretStoreResource{kind: kind}
		})
	}
	return out
}

type secretStoreResource struct {
	kind   secretStoreKind
	client *secretStoreClient
}

var _ resource.Resource = &secretStoreResource{}
var _ resource.ResourceWithImportState = &secretStoreResource{}

// secretStoreCloudOnly heads every secret store page.
const secretStoreCloudOnly = "~> **Requires Marmot Cloud or Marmot Enterprise.** Open-source Marmot " +
	"has no secret-store API, so this resource fails on apply. " +
	"[Marmot Cloud](https://cloud.marmotdata.io) includes it on every plan.\n\n"

func (r *secretStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret_store_" + r.kind.storeType
}

func (r *secretStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *secretStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// The identity depends on the name and on whether the store federates.
	identityInputs := rootPaths(append([]string{"name"}, r.kind.federation...)...)

	attrs := map[string]schema.Attribute{
		"name": schema.StringAttribute{
			MarkdownDescription: "Name of the store, unique per instance. A federated store's token " +
				"subject is `secretStore:{name}`, so changing it replaces the store.",
			Required: true,
			Validators: []validator.String{
				stringvalidator.LengthBetween(1, 255),
			},
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"id": schema.StringAttribute{
			MarkdownDescription: "Secret store ID",
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"subject": schema.StringAttribute{
			MarkdownDescription: "Subject of the tokens a federated store presents, `secretStore:{name}`. " +
				"This is what to grant on the cloud side. Null unless the store federates.",
			Computed:      true,
			PlanModifiers: []planmodifier.String{identityInputs},
		},
		"issuer": schema.StringAttribute{
			MarkdownDescription: "Issuer URL of the tokens a federated store presents. This is what " +
				"to register as an OIDC provider on the cloud side. Null unless the store federates.",
			Computed:      true,
			PlanModifiers: []planmodifier.String{identityInputs},
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
	}
	for _, f := range r.kind.fields {
		attrs[f.name] = f.attribute()
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: secretStoreCloudOnly + r.kind.description +
			"\n\nA store holds no secret values. `marmot_secret_store_" + r.kind.storeType + "_secret` " +
			"registers where a secret lives in it, and a `marmot_pipeline` reads that secret into " +
			"its config before each run. Roles on the store are granted with " +
			"`marmot_secret_store_iam_member`, `marmot_secret_store_iam_binding` and " +
			"`marmot_secret_store_iam_policy`: `secretStore.reader` reads secret values, " +
			"`secretStore.viewer` sees the store and its secrets, `secretStore.user` registers " +
			"secrets and attaches them to pipelines.\n\n" +
			"A federated store presents an OIDC token to its backend. Trust `issuer`, `subject` " +
			"and `audience` on the cloud side.",
		Attributes: attrs,
	}
}

// configFrom builds the config the API takes from the planned attributes.
// Null and unknown attributes are left out so the server applies its own
// defaults.
func (r *secretStoreResource) configFrom(ctx context.Context, src attrGetter, diags *diag.Diagnostics) map[string]any {
	config := make(map[string]any, len(r.kind.fields))
	for _, f := range r.kind.fields {
		var v types.String
		diags.Append(src.GetAttribute(ctx, path.Root(f.name), &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			config[f.name] = v.ValueString()
		}
	}
	return config
}

// fieldValue maps a config value onto its attribute. Absent or empty reads
// as null.
func fieldValue(raw any) types.String {
	if s, _ := raw.(string); s != "" {
		return types.StringValue(s)
	}
	return types.StringNull()
}

// identityValues maps the store's identity onto its attributes, null when
// there is none.
func identityValues(identity *secretStoreIdentity) (issuer, subject types.String) {
	if identity == nil {
		return types.StringNull(), types.StringNull()
	}
	return types.StringValue(identity.Issuer), types.StringValue(identity.Subject)
}

// persist writes a store to state as the server holds it.
func (r *secretStoreResource) persist(ctx context.Context, state *tfsdk.State, store *secretStore, diags *diag.Diagnostics) {
	diags.Append(state.SetAttribute(ctx, path.Root("id"), store.ID)...)
	diags.Append(state.SetAttribute(ctx, path.Root("name"), store.Name)...)
	diags.Append(state.SetAttribute(ctx, path.Root("created_at"), store.CreatedAt)...)
	diags.Append(state.SetAttribute(ctx, path.Root("updated_at"), store.UpdatedAt)...)

	issuer, subject := identityValues(store.Identity)
	diags.Append(state.SetAttribute(ctx, path.Root("issuer"), issuer)...)
	diags.Append(state.SetAttribute(ctx, path.Root("subject"), subject)...)

	for _, f := range r.kind.fields {
		diags.Append(state.SetAttribute(ctx, path.Root(f.name), fieldValue(store.Config[f.name]))...)
	}
}

func (r *secretStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var name types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &name)...)
	config := r.configFrom(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	store, err := r.client.CreateSecretStore(ctx, createSecretStoreRequest{
		Name:      name.ValueString(),
		StoreType: r.kind.storeType,
		Config:    config,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Secret Store", err.Error())
		return
	}

	tflog.Info(ctx, "Secret store created", map[string]any{
		"id":         store.ID,
		"name":       store.Name,
		"store_type": store.StoreType,
	})

	r.persist(ctx, &resp.State, store, &resp.Diagnostics)
}

func (r *secretStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	store, err := r.client.GetSecretStore(ctx, id.ValueString())
	if err != nil {
		if errors.Is(err, errNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Secret Store", err.Error())
		return
	}

	// A store of another type under this id is an import into the wrong
	// resource.
	if store.StoreType != r.kind.storeType {
		resp.Diagnostics.AddError("Secret Store Type Mismatch",
			fmt.Sprintf("Secret store %s is a %s store; import it into marmot_secret_store_%s instead.",
				store.ID, store.StoreType, store.StoreType))
		return
	}

	r.persist(ctx, &resp.State, store, &resp.Diagnostics)
}

func (r *secretStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	config := r.configFrom(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// The whole config goes every time; the server replaces what it has with
	// what is sent.
	store, err := r.client.UpdateSecretStore(ctx, id.ValueString(), updateSecretStoreRequest{Config: config})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Secret Store", err.Error())
		return
	}

	tflog.Info(ctx, "Secret store updated", map[string]any{
		"id":   store.ID,
		"name": store.Name,
	})

	r.persist(ctx, &resp.State, store, &resp.Diagnostics)
}

func (r *secretStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Already gone is fine. A store with a secret a pipeline still references
	// is refused.
	if err := r.client.DeleteSecretStore(ctx, id.ValueString()); err != nil && !errors.Is(err, errNotFound) {
		resp.Diagnostics.AddError("Unable to Delete Secret Store", err.Error())
		return
	}

	tflog.Info(ctx, "Secret store deleted", map[string]any{
		"id": id.ValueString(),
	})
}

func (r *secretStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
